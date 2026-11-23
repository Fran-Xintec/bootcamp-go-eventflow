package entity

import (
	"fmt"
	"strings"

	"eventflow/pkg/apperrors"

	"github.com/google/uuid"
)

type Status string

// El pedido nace "created" y pasa a "confirmed" cuando el worker procesa su evento.
const (
	StatusCreated   Status = "created"
	StatusConfirmed Status = "confirmed"
)

// Límites razonables para no recibir pedidos absurdos. También evitan que
// quantity × precio se desborde al sumar el total en int64.
const (
	MaxItems          = 100
	MaxQuantity       = 100_000
	MaxUnitPriceCents = 1_000_000_000
)

// Item es una línea del pedido. El precio va en centavos para no usar decimales.
type Item struct {
	SKU            string
	Quantity       int
	UnitPriceCents int64
}

type Order struct {
	ID         string
	CustomerID string
	Items      []Item
	TotalCents int64
	Status     Status
}

// NewOrder valida los datos y arma un pedido nuevo con su ID, su total y el estado inicial.
// Las reglas de negocio viven aquí y no en el handler, para poder probarlas sin HTTP.
func NewOrder(customerID string, items []Item) (*Order, error) {
	if strings.TrimSpace(customerID) == "" {
		return nil, apperrors.NewValidationError("ORDER_CUSTOMER_REQUIRED", "customer_id es obligatorio")
	}
	if len(items) == 0 {
		return nil, apperrors.NewValidationError("ORDER_ITEMS_REQUIRED", "se requiere al menos un artículo")
	}
	if len(items) > MaxItems {
		return nil, apperrors.NewValidationError("ORDER_TOO_MANY_ITEMS", fmt.Sprintf("un pedido puede tener como máximo %d artículos", MaxItems))
	}

	var total int64
	for i, it := range items {
		if strings.TrimSpace(it.SKU) == "" {
			return nil, apperrors.NewValidationError("ORDER_ITEM_INVALID", fmt.Sprintf("artículo %d: sku es obligatorio", i))
		}
		if it.Quantity <= 0 || it.Quantity > MaxQuantity {
			return nil, apperrors.NewValidationError("ORDER_ITEM_INVALID", fmt.Sprintf("artículo %d: quantity debe estar entre 1 y %d", i, MaxQuantity))
		}
		if it.UnitPriceCents <= 0 || it.UnitPriceCents > MaxUnitPriceCents {
			return nil, apperrors.NewValidationError("ORDER_ITEM_INVALID", fmt.Sprintf("artículo %d: unit_price_cents debe estar entre 1 y %d", i, MaxUnitPriceCents))
		}
		total += int64(it.Quantity) * it.UnitPriceCents
	}

	return &Order{
		ID:         uuid.NewString(),
		CustomerID: customerID,
		Items:      items,
		TotalCents: total,
		Status:     StatusCreated,
	}, nil
}
