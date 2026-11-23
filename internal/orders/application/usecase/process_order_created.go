package usecase

import (
	"context"

	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/event"
	"eventflow/internal/orders/domain/repository"

	"go.uber.org/zap"
)

// Notifier es lo que el worker necesita para avisar al cliente. La implementación
// real (correo, SMS) queda fuera del desafío: hoy es una simulación.
type Notifier interface {
	Notify(ctx context.Context, order *entity.Order) error
}

// ProcessOrderCreatedUseCase atiende el evento order.created: notifica y confirma el pedido.
type ProcessOrderCreatedUseCase struct {
	repo     repository.OrderRepository
	notifier Notifier
	log      *zap.Logger
}

func NewProcessOrderCreatedUseCase(repo repository.OrderRepository, notifier Notifier, log *zap.Logger) *ProcessOrderCreatedUseCase {
	return &ProcessOrderCreatedUseCase{repo: repo, notifier: notifier, log: log}
}

// Execute devuelve nil solo cuando el evento quedó procesado (o ya lo estaba): en
// ese caso el consumidor confirma el mensaje (ACK). Cualquier error provoca reintento.
//
// El broker entrega "al menos una vez", así que el mismo evento puede llegar repetido:
// el id del evento se guarda en la misma transacción que el cambio de estado, y un
// evento ya registrado se ignora. Si el proceso cae entre notificar y confirmar, la
// notificación puede repetirse (límite conocido).
func (uc *ProcessOrderCreatedUseCase) Execute(ctx context.Context, evt event.OrderCreated) error {
	processed, err := uc.repo.IsEventProcessed(ctx, evt.ID)
	if err != nil {
		return err
	}
	if processed {
		uc.log.Info("evento duplicado, se ignora", zap.String("request_id", evt.RequestID), zap.String("event_id", evt.ID), zap.String("order_id", evt.OrderID))
		return nil
	}

	order, err := uc.repo.FindByID(ctx, evt.OrderID)
	if err != nil {
		return err
	}

	if err := uc.notifier.Notify(ctx, order); err != nil {
		return err
	}

	confirmed, err := uc.repo.ConfirmOrder(ctx, evt.ID, evt.OrderID)
	if err != nil {
		return err
	}
	if !confirmed {
		uc.log.Info("evento duplicado, se ignora", zap.String("request_id", evt.RequestID), zap.String("event_id", evt.ID), zap.String("order_id", evt.OrderID))
		return nil
	}

	uc.log.Info("pedido confirmado", zap.String("request_id", evt.RequestID), zap.String("event_id", evt.ID), zap.String("order_id", evt.OrderID))
	return nil
}
