package event

import (
	"time"

	"github.com/google/uuid"
)

// TypeOrderCreated es el tipo del evento que se publica al crear un pedido.
// Si el formato cambia en el futuro se versiona en el nombre (order.created.v2).
const TypeOrderCreated = "order.created"

// OrderCreated es el mensaje que viaja por el broker: lleva el id del pedido y no
// sus datos, el worker los busca en la base.
type OrderCreated struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurred_at"`
	OrderID    string    `json:"order_id"`
	// RequestID es el X-Request-Id de la petición que creó el pedido: permite seguir
	// la misma operación en los logs de la API y del worker.
	RequestID string `json:"request_id,omitempty"`
}

func NewOrderCreated(orderID string) OrderCreated {
	return OrderCreated{
		ID:         uuid.NewString(),
		Type:       TypeOrderCreated,
		OccurredAt: time.Now().UTC(),
		OrderID:    orderID,
	}
}
