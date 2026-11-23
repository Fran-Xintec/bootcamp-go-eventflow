package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"eventflow/internal/orders/domain/event"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"
)

// fakeAck registra qué hizo el consumidor con el mensaje (ACK, NACK con o sin reencolar).
type fakeAck struct {
	acked, nacked, requeued bool
}

func (f *fakeAck) Ack(uint64, bool) error { f.acked = true; return nil }
func (f *fakeAck) Nack(_ uint64, _ bool, requeue bool) error {
	f.nacked, f.requeued = true, requeue
	return nil
}
func (f *fakeAck) Reject(uint64, bool) error { return nil }

type fakeProcessor struct {
	failures int // cuántas veces seguidas falla antes de funcionar (-1 = siempre)
	calls    int
}

func (p *fakeProcessor) Execute(context.Context, event.OrderCreated) error {
	p.calls++
	if p.failures < 0 || p.calls <= p.failures {
		return errors.New("fallo simulado")
	}
	return nil
}

func newTestConsumer(p OrderCreatedProcessor) *RabbitConsumer {
	delays := []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}
	return NewRabbitConsumer("", p, zap.NewNop(), delays, time.Second)
}

func delivery(t *testing.T, ack *fakeAck, body []byte) amqp.Delivery {
	t.Helper()
	return amqp.Delivery{Acknowledger: ack, DeliveryTag: 1, Body: body}
}

func validBody(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal(event.NewOrderCreated("6f1b0f7e-8f5c-4c37-9c63-1f0d9d0a1111"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestConsumer_AcksAfterSuccess(t *testing.T) {
	proc, ack := &fakeProcessor{}, &fakeAck{}

	newTestConsumer(proc).handle(context.Background(), delivery(t, ack, validBody(t)))

	if !ack.acked || ack.nacked || proc.calls != 1 {
		t.Errorf("acked=%v nacked=%v calls=%d; se esperaba ACK con un solo intento", ack.acked, ack.nacked, proc.calls)
	}
}

// RF-11: un fallo transitorio se reintenta y, al funcionar, el mensaje se confirma.
func TestConsumer_RetriesTransientFailureThenAcks(t *testing.T) {
	proc, ack := &fakeProcessor{failures: 2}, &fakeAck{}

	newTestConsumer(proc).handle(context.Background(), delivery(t, ack, validBody(t)))

	if !ack.acked || ack.nacked || proc.calls != 3 {
		t.Errorf("acked=%v nacked=%v calls=%d; se esperaba ACK en el tercer intento", ack.acked, ack.nacked, proc.calls)
	}
}

// RF-11 y RF-12: al agotar los reintentos (1 inicial + 3) el mensaje se rechaza sin reencolar (va a la DLQ).
func TestConsumer_ExhaustedRetriesGoToDeadLetter(t *testing.T) {
	proc, ack := &fakeProcessor{failures: -1}, &fakeAck{}

	newTestConsumer(proc).handle(context.Background(), delivery(t, ack, validBody(t)))

	if ack.acked || !ack.nacked || ack.requeued {
		t.Errorf("acked=%v nacked=%v requeued=%v; se esperaba NACK sin reencolar", ack.acked, ack.nacked, ack.requeued)
	}
	if proc.calls != 4 {
		t.Errorf("intentos = %d, se esperaban 4 (1 inicial + 3 reintentos)", proc.calls)
	}
}

func TestConsumer_InvalidMessageGoesToDeadLetterWithoutRetries(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{"no es JSON", []byte("esto no es json")},
		{"sin id de evento", []byte(`{"order_id":"abc"}`)},
		{"sin id de pedido", []byte(`{"id":"abc"}`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			proc, ack := &fakeProcessor{}, &fakeAck{}

			newTestConsumer(proc).handle(context.Background(), delivery(t, ack, tc.body))

			if !ack.nacked || ack.requeued || ack.acked || proc.calls != 0 {
				t.Errorf("acked=%v nacked=%v requeued=%v calls=%d; se esperaba NACK directo a la DLQ", ack.acked, ack.nacked, ack.requeued, proc.calls)
			}
		})
	}
}

// Si el worker se apaga durante la espera de un reintento, el mensaje vuelve a la cola.
func TestConsumer_ShutdownRequeuesMessage(t *testing.T) {
	proc, ack := &fakeProcessor{failures: -1}, &fakeAck{}
	c := NewRabbitConsumer("", proc, zap.NewNop(), []time.Duration{time.Hour}, time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.handle(ctx, delivery(t, ack, validBody(t)))

	if !ack.nacked || !ack.requeued || ack.acked {
		t.Errorf("acked=%v nacked=%v requeued=%v; se esperaba NACK con reencolado", ack.acked, ack.nacked, ack.requeued)
	}
}
