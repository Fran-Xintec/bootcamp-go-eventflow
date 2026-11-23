package usecase

import (
	"context"
	"errors"
	"testing"

	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/event"

	"go.uber.org/zap"
)

type fakeNotifier struct {
	calls int
	err   error
}

func (n *fakeNotifier) Notify(context.Context, *entity.Order) error {
	n.calls++
	return n.err
}

func repoWithOrder(t *testing.T) (*fakeRepo, *entity.Order) {
	t.Helper()
	repo := newFakeRepo()
	order, err := entity.NewOrder("cliente-1", []entity.Item{{SKU: "A", Quantity: 1, UnitPriceCents: 500}})
	if err != nil {
		t.Fatal(err)
	}
	repo.orders[order.ID] = order
	return repo, order
}

func TestCreateOrder_PublishesEvent(t *testing.T) {
	repo, publisher := newFakeRepo(), &fakePublisher{}
	uc := NewCreateOrderUseCase(repo, publisher, zap.NewNop())

	res, _, err := uc.Execute(context.Background(), validRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if publisher.count() != 1 {
		t.Fatalf("eventos publicados = %d, se esperaba 1", publisher.count())
	}
	evt := publisher.events[0]
	if evt.Type != event.TypeOrderCreated || evt.OrderID != res.ID || evt.ID == "" || evt.OccurredAt.IsZero() {
		t.Errorf("evento inesperado: %+v", evt)
	}
}

func TestProcessOrderCreated_ConfirmsOrder(t *testing.T) {
	repo, order := repoWithOrder(t)
	notifier := &fakeNotifier{}
	uc := NewProcessOrderCreatedUseCase(repo, notifier, zap.NewNop())

	if err := uc.Execute(context.Background(), event.NewOrderCreated(order.ID)); err != nil {
		t.Fatalf("Execute() error inesperado: %v", err)
	}

	if order.Status != entity.StatusConfirmed {
		t.Errorf("status = %q, se esperaba confirmed", order.Status)
	}
	if notifier.calls != 1 {
		t.Errorf("notificaciones = %d, se esperaba 1", notifier.calls)
	}
}

// Q-02: el mismo evento recibido dos veces (entrega "al menos una vez") tiene efecto una sola vez.
func TestProcessOrderCreated_DuplicateEventIsIgnored(t *testing.T) {
	repo, order := repoWithOrder(t)
	notifier := &fakeNotifier{}
	uc := NewProcessOrderCreatedUseCase(repo, notifier, zap.NewNop())
	evt := event.NewOrderCreated(order.ID)

	if err := uc.Execute(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if err := uc.Execute(context.Background(), evt); err != nil {
		t.Fatalf("el duplicado no debe dar error (el mensaje se confirma): %v", err)
	}

	if notifier.calls != 1 {
		t.Errorf("notificaciones = %d, el duplicado no debe notificar de nuevo", notifier.calls)
	}
}

func TestProcessOrderCreated_NotifierFailureDoesNotConfirm(t *testing.T) {
	repo, order := repoWithOrder(t)
	boom := errors.New("servicio de notificaciones caído")
	uc := NewProcessOrderCreatedUseCase(repo, &fakeNotifier{err: boom}, zap.NewNop())

	err := uc.Execute(context.Background(), event.NewOrderCreated(order.ID))

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, se esperaba el error del notificador (provoca reintento)", err)
	}
	if order.Status != entity.StatusCreated {
		t.Errorf("status = %q, el pedido no debe confirmarse si la notificación falló", order.Status)
	}
}

func TestProcessOrderCreated_UnknownOrderFails(t *testing.T) {
	uc := NewProcessOrderCreatedUseCase(newFakeRepo(), &fakeNotifier{}, zap.NewNop())

	err := uc.Execute(context.Background(), event.NewOrderCreated("6f1b0f7e-8f5c-4c37-9c63-1f0d9d0a1111"))

	if err == nil {
		t.Fatal("se esperaba un error: el pedido del evento no existe")
	}
}
