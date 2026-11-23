package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"eventflow/internal/orders/application/dto"
	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/event"
	"eventflow/internal/orders/domain/repository"
	"eventflow/pkg/apperrors"
	"eventflow/pkg/requestid"

	"go.uber.org/zap"
)

// EventPublisher es lo que el caso de uso necesita para avisar que un pedido se creó.
// La interfaz se define aquí (donde se usa) y no en el paquete que la implementa.
type EventPublisher interface {
	Publish(ctx context.Context, evt event.OrderCreated) error
}

// Tope de tiempo para publicar el evento; es propio porque no debe depender de que
// el cliente siga conectado (el pedido ya está guardado).
const publishTimeout = 4 * time.Second

type CreateOrderUseCase struct {
	repo      repository.OrderRepository
	publisher EventPublisher
	log       *zap.Logger
}

func NewCreateOrderUseCase(repo repository.OrderRepository, publisher EventPublisher, log *zap.Logger) *CreateOrderUseCase {
	return &CreateOrderUseCase{repo: repo, publisher: publisher, log: log}
}

const maxIdempotencyKeyLength = 128

// Execute crea el pedido. idempotencyKey es nil si el cliente no envió la cabecera.
// El segundo valor devuelto es true cuando la petición era un reintento de una
// anterior (misma clave y mismo cuerpo): en ese caso no se crea nada ni se notifica,
// y se devuelve la respuesta original.
func (uc *CreateOrderUseCase) Execute(ctx context.Context, req dto.CreateOrderRequest, idempotencyKey *string) (*dto.CreateOrderResponse, bool, error) {
	var idem *repository.Idempotency
	if idempotencyKey != nil {
		key := *idempotencyKey
		if len(key) < 1 || len(key) > maxIdempotencyKeyLength {
			return nil, false, apperrors.NewBadRequestError("IDEMPOTENCY_KEY_INVALID",
				fmt.Sprintf("Idempotency-Key debe tener entre 1 y %d caracteres", maxIdempotencyKeyLength))
		}
		hash, err := hashRequest(req)
		if err != nil {
			return nil, false, err
		}
		idem = &repository.Idempotency{Key: key, RequestHash: hash}

		// Camino normal de un reintento: la clave ya existe.
		res, found, err := uc.replay(ctx, idem)
		if err != nil || found {
			return res, found, err
		}
	}

	items := make([]entity.Item, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, entity.Item{SKU: it.SKU, Quantity: it.Quantity, UnitPriceCents: it.UnitPriceCents})
	}

	order, err := entity.NewOrder(req.CustomerID, items)
	if err != nil {
		return nil, false, err
	}

	if err := uc.repo.Save(ctx, order, idem); err != nil {
		// Otra petición con la misma clave se adelantó (reintentos simultáneos):
		// se responde igual que a un reintento normal.
		if errors.Is(err, repository.ErrIdempotencyKeyTaken) {
			res, found, replayErr := uc.replay(ctx, idem)
			if replayErr == nil && !found {
				replayErr = errors.New("la clave de idempotencia existe pero no se pudo leer")
			}
			return res, found, replayErr
		}
		return nil, false, err
	}

	uc.publishCreated(ctx, order.ID)

	return dto.NewCreateOrderResponse(order), false, nil
}

// replay busca una petición anterior con la misma clave. Si existe y el cuerpo es
// el mismo devuelve el pedido original; si el cuerpo cambió es un error 422.
func (uc *CreateOrderUseCase) replay(ctx context.Context, idem *repository.Idempotency) (*dto.CreateOrderResponse, bool, error) {
	stored, err := uc.repo.FindByIdempotencyKey(ctx, idem.Key)
	if errors.Is(err, repository.ErrIdempotencyKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	if stored.RequestHash != idem.RequestHash {
		return nil, false, apperrors.NewValidationError("IDEMPOTENCY_KEY_REUSED",
			"la Idempotency-Key ya se usó con un cuerpo distinto")
	}

	order, err := uc.repo.FindByID(ctx, stored.OrderID)
	if err != nil {
		return nil, false, err
	}
	return dto.NewCreateOrderResponse(order), true, nil
}

// hashRequest saca una huella del contenido del pedido (no de los bytes crudos),
// para que dos cuerpos iguales con distinto espaciado cuenten como el mismo.
func hashRequest(req dto.CreateOrderRequest) (string, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("calculando la huella de la petición: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// publishCreated publica el evento después de guardar el pedido. Si el broker no
// responde, el pedido ya quedó guardado: se registra el error y no se falla la
// petición (límite conocido, ver README: una mejora sería un outbox transaccional).
func (uc *CreateOrderUseCase) publishCreated(ctx context.Context, orderID string) {
	evt := event.NewOrderCreated(orderID)
	evt.RequestID = requestid.From(ctx)

	pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
	defer cancel()

	if err := uc.publisher.Publish(pctx, evt); err != nil {
		uc.log.Error("no se pudo publicar el evento del pedido",
			zap.String("request_id", evt.RequestID), zap.String("order_id", orderID), zap.String("event_id", evt.ID), zap.Error(err))
	}
}
