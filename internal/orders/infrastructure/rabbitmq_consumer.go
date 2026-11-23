package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"eventflow/internal/orders/domain/event"
	"eventflow/pkg/requestid"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// OrderCreatedProcessor es lo que el consumidor necesita del caso de uso.
type OrderCreatedProcessor interface {
	Execute(ctx context.Context, evt event.OrderCreated) error
}

// Esperas entre reintentos (ver "ACK/NACK y duplicados" en el README): un intento inicial y 3 reintentos.
var DefaultRetryDelays = []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second}

// RabbitConsumer consume la cola orders.created. Política de confirmación:
//   - ACK solo cuando el procesamiento terminó bien (o el evento ya estaba procesado).
//   - Si falla, se reintenta con espera creciente; al agotar los reintentos se hace
//     NACK sin reencolar y RabbitMQ envía el mensaje a la cola de fallidos (DLQ).
//   - Si el mensaje no es un evento válido, va directo a la DLQ (reintentar no lo arregla).
//   - Si el worker se apaga durante una espera, el mensaje vuelve a la cola (no se pierde).
type RabbitConsumer struct {
	url            string
	processor      OrderCreatedProcessor
	log            *zap.Logger
	retryDelays    []time.Duration
	attemptTimeout time.Duration
}

func NewRabbitConsumer(url string, processor OrderCreatedProcessor, log *zap.Logger, retryDelays []time.Duration, attemptTimeout time.Duration) *RabbitConsumer {
	return &RabbitConsumer{url: url, processor: processor, log: log, retryDelays: retryDelays, attemptTimeout: attemptTimeout}
}

// Run consume mensajes hasta que ctx se cancele (apagado ordenado) y termina el
// mensaje en curso antes de volver. Devuelve error si se pierde la conexión.
func (c *RabbitConsumer) Run(ctx context.Context) error {
	conn, err := c.connect(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil // se pidió apagar mientras se esperaba al broker: no es un error
		}
		return err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("abriendo el canal: %w", err)
	}
	defer ch.Close()

	if err := declareTopology(ch); err != nil {
		return err
	}
	// De a un mensaje por vez: los reintentos de uno no se mezclan con los de otro.
	if err := ch.Qos(1, 0, false); err != nil {
		return fmt.Errorf("configurando prefetch: %w", err)
	}
	msgs, err := ch.Consume(QueueOrderCreated, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consumiendo %s: %w", QueueOrderCreated, err)
	}
	c.log.Info("worker escuchando la cola", zap.String("queue", QueueOrderCreated))

	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-msgs:
			if !ok {
				return fmt.Errorf("se cerró el canal de RabbitMQ")
			}
			c.handle(ctx, d)
		}
	}
}

// connect espera al broker (el worker puede arrancar antes que RabbitMQ).
func (c *RabbitConsumer) connect(ctx context.Context) (*amqp.Connection, error) {
	for {
		conn, err := dial(c.url)
		if err == nil {
			return conn, nil
		}
		c.log.Warn("RabbitMQ todavía no responde, se reintenta", zap.Error(err))
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *RabbitConsumer) handle(ctx context.Context, d amqp.Delivery) {
	var evt event.OrderCreated
	if err := json.Unmarshal(d.Body, &evt); err != nil || evt.ID == "" || evt.OrderID == "" {
		c.log.Error("mensaje inválido, va a la cola de fallidos", zap.ByteString("body", d.Body), zap.Error(err))
		_ = d.Nack(false, false)
		return
	}

	var err error
	for attempt := 0; ; attempt++ {
		err = c.process(ctx, evt)
		if err == nil {
			_ = d.Ack(false)
			return
		}
		if attempt >= len(c.retryDelays) {
			break
		}

		wait := c.retryDelays[attempt]
		c.log.Warn("falló el procesamiento del evento, se reintenta",
			zap.String("request_id", evt.RequestID), zap.String("event_id", evt.ID), zap.String("order_id", evt.OrderID),
			zap.Int("retry", attempt+1), zap.Duration("wait", wait), zap.Error(err))

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			_ = d.Nack(false, true) // apagado: el mensaje vuelve a la cola
			return
		}
	}

	c.log.Error("se agotaron los reintentos, el mensaje va a la cola de fallidos",
		zap.String("request_id", evt.RequestID), zap.String("event_id", evt.ID), zap.String("order_id", evt.OrderID),
		zap.Int("attempts", len(c.retryDelays)+1), zap.Error(err))
	_ = d.Nack(false, false)
}

func (c *RabbitConsumer) process(ctx context.Context, evt event.OrderCreated) error {
	// El id de correlación del evento viaja en el context hasta el notificador.
	pctx, cancel := context.WithTimeout(requestid.With(ctx, evt.RequestID), c.attemptTimeout)
	defer cancel()
	return c.processor.Execute(pctx, evt)
}
