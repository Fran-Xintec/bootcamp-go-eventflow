package dto

import "eventflow/internal/orders/domain/entity"

// Lo que llega en POST /orders.
type CreateOrderRequest struct {
	CustomerID string        `json:"customer_id"`
	Items      []ItemRequest `json:"items"`
}

type ItemRequest struct {
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

// Lo que devuelve POST /orders.
type CreateOrderResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	TotalCents int64  `json:"total_cents"`
}

// Lo que devuelve GET /orders/{id}.
type OrderResponse struct {
	ID         string         `json:"id"`
	CustomerID string         `json:"customer_id"`
	Status     string         `json:"status"`
	TotalCents int64          `json:"total_cents"`
	Items      []ItemResponse `json:"items"`
}

type ItemResponse struct {
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

func NewCreateOrderResponse(o *entity.Order) *CreateOrderResponse {
	return &CreateOrderResponse{ID: o.ID, Status: string(o.Status), TotalCents: o.TotalCents}
}

func NewOrderResponse(o *entity.Order) *OrderResponse {
	items := make([]ItemResponse, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, ItemResponse{SKU: it.SKU, Quantity: it.Quantity, UnitPriceCents: it.UnitPriceCents})
	}

	return &OrderResponse{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Status:     string(o.Status),
		TotalCents: o.TotalCents,
		Items:      items,
	}
}
