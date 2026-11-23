package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"eventflow/internal/orders/domain/event"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// Topología de RabbitMQ:
//
//	exchange orders --(order.created)--> cola orders.created
//	cola orders.created --(rechazados o agotados)--> exchange orders.dlx --> cola orders.created.dlq
const (
	exchangeOrders         = "orders"
	exchangeDeadLetter     = "orders.dlx"
	routingKeyOrderCreated = "order.created"

	QueueOrderCreated    = "orders.created"
	QueueOrderCreatedDLQ = "orders.created.dlq"
)

// declareTopology crea los exchanges y las colas si no existen. Es idempotente, así que
// la API y el worker pueden llamarlo al conectarse, sin importar quién arranque primero.
func declareTopology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(exchangeOrders, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declarando el exchange %s: %w", exchangeOrders, err)
	}
	if err := ch.ExchangeDeclare(exchangeDeadLetter, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declarando el exchange %s: %w", exchangeDeadLetter, err)
	}

	if _, err := ch.QueueDeclare(QueueOrderCreatedDLQ, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declarando la cola %s: %w", QueueOrderCreatedDLQ, err)
	}
	if err := ch.QueueBind(QueueOrderCreatedDLQ, routingKeyOrderCreated, exchangeDeadLetter, false, nil); err != nil {
		return fmt.Errorf("enlazando la cola %s: %w", QueueOrderCreatedDLQ, err)
	}

	// Lo que se rechaza sin reencolar (NACK) pasa al exchange de fallidos.
	args := amqp.Table{
		"x-dead-letter-exchange":    exchangeDeadLetter,
		"x-dead-letter-routing-key": routingKeyOrderCreated,
	}
	if _, err := ch.QueueDeclare(QueueOrderCreated, true, false, false, false, args); err != nil {
		return fmt.Errorf("declarando la cola %s: %w", QueueOrderCreated, err)
	}
	if err := ch.QueueBind(QueueOrderCreated, routingKeyOrderCreated, exchangeOrders, false, nil); err != nil {
		return fmt.Errorf("enlazando la cola %s: %w", QueueOrderCreated, err)
	}
	return nil
}

func dial(url string) (*amqp.Connection, error) {
	return amqp.DialConfig(url, amqp.Config{Dial: amqp.DefaultDial(1500 * time.Millisecond)})
}

// --- Publicador (lado de la API) ---

const (
	publishAttempts = 3
	publishBackoff  = 200 * time.Millisecond
)

// RabbitPublisher publica eventos en RabbitMQ. Se conecta la primera vez que se usa
// (la API arranca aunque el broker esté caído) y se reconecta si la conexión se pierde.
// Usa confirmaciones del broker: Publish solo devuelve nil si el broker aceptó el mensaje.
type RabbitPublisher struct {
	url string
	log *zap.Logger

	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewRabbitPublisher(url string, log *zap.Logger) *RabbitPublisher {
	return &RabbitPublisher{url: url, log: log}
}

// Publish intenta hasta 3 veces (con una pausa corta y reconexión) antes de rendirse.
func (p *RabbitPublisher) Publish(ctx context.Context, evt event.OrderCreated) error {
	body, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("serializando el evento: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= publishAttempts; attempt++ {
		lastErr = p.publishOnce(ctx, evt, body)
		if lastErr == nil {
			return nil
		}
		p.log.Warn("falló la publicación del evento",
			zap.Int("attempt", attempt), zap.String("event_id", evt.ID), zap.Error(lastErr))

		if attempt < publishAttempts {
			select {
			case <-time.After(publishBackoff * time.Duration(attempt)):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("el broker no aceptó el evento tras %d intentos: %w", publishAttempts, lastErr)
}

func (p *RabbitPublisher) publishOnce(ctx context.Context, evt event.OrderCreated, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureConnected(); err != nil {
		return err
	}

	confirm, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, exchangeOrders, routingKeyOrderCreated, false, false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // el mensaje sobrevive a un reinicio del broker
			MessageId:    evt.ID,
			Type:         evt.Type,
			Timestamp:    evt.OccurredAt,
			Body:         body,
		})
	if err != nil {
		p.reset()
		return err
	}

	acked, err := confirm.WaitContext(ctx)
	if err != nil {
		p.reset()
		return err
	}
	if !acked {
		return errors.New("el broker rechazó el mensaje (nack)")
	}
	return nil
}

func (p *RabbitPublisher) ensureConnected() error {
	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil && !p.ch.IsClosed() {
		return nil
	}
	p.reset()

	conn, err := dial(p.url)
	if err != nil {
		return fmt.Errorf("conectando con RabbitMQ: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("abriendo el canal: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = conn.Close()
		return fmt.Errorf("activando las confirmaciones: %w", err)
	}
	if err := declareTopology(ch); err != nil {
		_ = conn.Close()
		return err
	}

	p.conn, p.ch = conn, ch
	return nil
}

func (p *RabbitPublisher) reset() {
	if p.conn != nil {
		_ = p.conn.Close()
	}
	p.conn, p.ch = nil, nil
}

// Close cierra la conexión (al apagar la API).
func (p *RabbitPublisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reset()
}
