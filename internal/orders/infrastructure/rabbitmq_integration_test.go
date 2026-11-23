package infrastructure

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"eventflow/internal/notifications/infrastructure"
	"eventflow/internal/orders/application/usecase"
	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/event"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// Estas pruebas usan el RabbitMQ y la base del docker compose; se saltan si no hay
// RABBITMQ_URL (levantar con `docker compose up -d db rabbitmq`).
func rabbitURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("RABBITMQ_URL")
	if url == "" {
		t.Skip("RABBITMQ_URL no definido: se salta la prueba de integración con RabbitMQ")
	}
	return url
}

// purge deja las colas vacías para que la prueba no dependa de mensajes anteriores.
func purge(t *testing.T, url string) {
	t.Helper()
	conn, err := dial(url)
	if err != nil {
		t.Fatalf("conectando con RabbitMQ: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	if err := declareTopology(ch); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{QueueOrderCreated, QueueOrderCreatedDLQ} {
		if _, err := ch.QueuePurge(q, false); err != nil {
			t.Fatal(err)
		}
	}
}

func dlqSize(t *testing.T, url string) int {
	t.Helper()
	conn, err := dial(url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	q, err := ch.QueueInspect(QueueOrderCreatedDLQ)
	if err != nil {
		t.Fatal(err)
	}
	return q.Messages
}

// startWorker arma el consumidor real con la base real y lo deja corriendo hasta el fin de la prueba.
func startWorker(t *testing.T, url string, repo *PostgresOrderRepository) {
	t.Helper()
	notifier := infrastructure.NewLogNotifier(10*time.Millisecond, zap.NewNop())
	processor := usecase.NewProcessOrderCreatedUseCase(repo, notifier, zap.NewNop())
	delays := []time.Duration{20 * time.Millisecond, 20 * time.Millisecond, 20 * time.Millisecond}
	consumer := NewRabbitConsumer(url, processor, zap.NewNop(), delays, 2*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = consumer.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("se agotó el tiempo esperando: %s", what)
}

func saveOrder(t *testing.T, repo *PostgresOrderRepository, customer string) *entity.Order {
	t.Helper()
	order, err := entity.NewOrder(customer, []entity.Item{{SKU: "A", Quantity: 1, UnitPriceCents: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), order, nil); err != nil {
		t.Fatal(err)
	}
	return order
}

func status(t *testing.T, repo *PostgresOrderRepository, id string) entity.Status {
	t.Helper()
	o, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return o.Status
}

// Camino completo con el broker real: se publica order.created, el worker lo consume,
// confirma el pedido en la base, y un duplicado del mismo evento no cambia nada.
func TestRabbitMQ_PublishConsumeAndDuplicate(t *testing.T) {
	url := rabbitURL(t)
	db := newTestDB(t)
	repo, _ := NewPostgresOrderRepository(db)
	purge(t, url)
	startWorker(t, url, repo)

	order := saveOrder(t, repo, "cliente-rabbit")
	t.Cleanup(func() { cleanup(t, db, order.ID) })

	publisher := NewRabbitPublisher(url, zap.NewNop())
	defer publisher.Close()
	evt := event.NewOrderCreated(order.ID)
	if err := publisher.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	waitFor(t, "que el pedido pase a confirmed", func() bool { return status(t, repo, order.ID) == entity.StatusConfirmed })

	// Duplicado: el mismo evento (mismo id) otra vez. Se confirma sin repetir el efecto.
	if err := publisher.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish duplicado: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	processed, err := repo.IsEventProcessed(context.Background(), evt.ID)
	if err != nil || !processed {
		t.Errorf("el evento debería figurar como procesado (err %v)", err)
	}
	if n := dlqSize(t, url); n != 0 {
		t.Errorf("la DLQ tiene %d mensajes, se esperaba 0", n)
	}
}

// Fallo permanente: se agotan los reintentos y el mensaje llega a la cola de fallidos.
func TestRabbitMQ_ExhaustedRetriesEndInDeadLetterQueue(t *testing.T) {
	url := rabbitURL(t)
	db := newTestDB(t)
	repo, _ := NewPostgresOrderRepository(db)
	purge(t, url)
	startWorker(t, url, repo)

	order := saveOrder(t, repo, "demo-fallo-permanente")
	t.Cleanup(func() { cleanup(t, db, order.ID) })

	publisher := NewRabbitPublisher(url, zap.NewNop())
	defer publisher.Close()
	if err := publisher.Publish(context.Background(), event.NewOrderCreated(order.ID)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	waitFor(t, "que el mensaje llegue a la DLQ", func() bool { return dlqSize(t, url) == 1 })
	if s := status(t, repo, order.ID); s != entity.StatusCreated {
		t.Errorf("status = %q, el pedido no debe confirmarse", s)
	}

	// El mensaje en la DLQ conserva el evento original, para investigarlo.
	conn, _ := dial(url)
	defer conn.Close()
	ch, _ := conn.Channel()
	defer ch.Close()
	msg, ok, err := ch.Get(QueueOrderCreatedDLQ, true)
	if err != nil || !ok {
		t.Fatalf("no se pudo leer la DLQ: %v (ok=%v)", err, ok)
	}
	var evt event.OrderCreated
	if err := json.Unmarshal(msg.Body, &evt); err != nil || evt.OrderID != order.ID {
		t.Errorf("el mensaje de la DLQ no es el evento original: %s", msg.Body)
	}
}

// Q-03: con el broker caído Publish falla (la API lo registra y responde 201 igual).
func TestRabbitMQ_PublishFailsWhenBrokerIsDown(t *testing.T) {
	publisher := NewRabbitPublisher("amqp://guest:guest@127.0.0.1:1/", zap.NewNop())
	defer publisher.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := publisher.Publish(ctx, event.NewOrderCreated("x")); err == nil {
		t.Fatal("se esperaba un error con el broker caído")
	}
}

var _ amqp.Acknowledger = (*fakeAck)(nil)
