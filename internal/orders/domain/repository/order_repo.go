package repository

import (
	"context"
	"errors"

	"eventflow/internal/orders/domain/entity"
	"eventflow/pkg/apperrors"
)

// ErrOrderNotFound lo devuelve el repositorio cuando no existe el pedido.
// Es un error de dominio de tipo NOT_FOUND, así que el mapper lo traduce a 404.
var ErrOrderNotFound = apperrors.NewNotFoundError("ORDER_NOT_FOUND", "Pedido no encontrado")

// Errores internos de la idempotencia: nunca llegan al cliente, el caso de uso los resuelve.
var (
	ErrIdempotencyKeyNotFound = errors.New("clave de idempotencia no encontrada")
	ErrIdempotencyKeyTaken    = errors.New("la clave de idempotencia ya fue usada por otra petición")
)

// Idempotency es la clave que envía el cliente y la huella (hash) de su cuerpo.
type Idempotency struct {
	Key         string
	RequestHash string
}

// StoredKey es lo que quedó guardado de una clave: la huella y el pedido que creó.
type StoredKey struct {
	RequestHash string
	OrderID     string
}

// OrderRepository es el contrato que necesita la capa de aplicación.
// La implementación (PostgreSQL) vive en infrastructure; así los casos de uso
// no dependen de la base de datos y se pueden probar con un repositorio fake.
type OrderRepository interface {
	// Save guarda el pedido y, si idem no es nil, su clave de idempotencia en la
	// misma transacción. Devuelve ErrIdempotencyKeyTaken si la clave ya existe.
	Save(ctx context.Context, order *entity.Order, idem *Idempotency) error
	FindByID(ctx context.Context, id string) (*entity.Order, error)
	// FindByIdempotencyKey devuelve ErrIdempotencyKeyNotFound si la clave no existe.
	FindByIdempotencyKey(ctx context.Context, key string) (*StoredKey, error)

	// IsEventProcessed dice si el worker ya procesó un evento (para ignorar duplicados).
	IsEventProcessed(ctx context.Context, eventID string) (bool, error)
	// ConfirmOrder registra el evento como procesado y pasa el pedido a "confirmed",
	// todo en una transacción. Devuelve false si el evento ya estaba registrado
	// (otro mensaje igual se adelantó) y no hace nada.
	ConfirmOrder(ctx context.Context, eventID, orderID string) (bool, error)
}
