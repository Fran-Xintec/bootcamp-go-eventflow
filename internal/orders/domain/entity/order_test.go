package entity

import (
	"errors"
	"strings"
	"testing"

	"eventflow/pkg/apperrors"

	"github.com/google/uuid"
)

func validItems() []Item {
	return []Item{
		{SKU: "A-1", Quantity: 2, UnitPriceCents: 1500},
		{SKU: "B-2", Quantity: 1, UnitPriceCents: 999},
	}
}

func TestNewOrder_Valid(t *testing.T) {
	o, err := NewOrder("cliente-1", validItems())
	if err != nil {
		t.Fatalf("NewOrder() devolvió error inesperado: %v", err)
	}

	if _, err := uuid.Parse(o.ID); err != nil {
		t.Errorf("ID = %q, se esperaba un UUID", o.ID)
	}
	if o.Status != StatusCreated {
		t.Errorf("Status = %q, se esperaba %q", o.Status, StatusCreated)
	}
	// 2 × 1500 + 1 × 999
	if o.TotalCents != 3999 {
		t.Errorf("TotalCents = %d, se esperaba 3999", o.TotalCents)
	}
}

func TestNewOrder_Invalid(t *testing.T) {
	tests := []struct {
		name       string
		customerID string
		items      []Item
		wantCode   string
	}{
		{"cliente vacío", "", validItems(), "ORDER_CUSTOMER_REQUIRED"},
		{"cliente solo espacios", "   ", validItems(), "ORDER_CUSTOMER_REQUIRED"},
		{"lista nil", "c1", nil, "ORDER_ITEMS_REQUIRED"},
		{"lista vacía", "c1", []Item{}, "ORDER_ITEMS_REQUIRED"},
		{"demasiados artículos", "c1", make([]Item, MaxItems+1), "ORDER_TOO_MANY_ITEMS"},
		{"sku vacío", "c1", []Item{{SKU: "", Quantity: 1, UnitPriceCents: 100}}, "ORDER_ITEM_INVALID"},
		{"cantidad cero", "c1", []Item{{SKU: "A", Quantity: 0, UnitPriceCents: 100}}, "ORDER_ITEM_INVALID"},
		{"cantidad negativa", "c1", []Item{{SKU: "A", Quantity: -3, UnitPriceCents: 100}}, "ORDER_ITEM_INVALID"},
		{"cantidad sobre el máximo", "c1", []Item{{SKU: "A", Quantity: MaxQuantity + 1, UnitPriceCents: 100}}, "ORDER_ITEM_INVALID"},
		{"precio cero", "c1", []Item{{SKU: "A", Quantity: 1, UnitPriceCents: 0}}, "ORDER_ITEM_INVALID"},
		{"precio negativo", "c1", []Item{{SKU: "A", Quantity: 1, UnitPriceCents: -10}}, "ORDER_ITEM_INVALID"},
		{"precio sobre el máximo", "c1", []Item{{SKU: "A", Quantity: 1, UnitPriceCents: MaxUnitPriceCents + 1}}, "ORDER_ITEM_INVALID"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, err := NewOrder(tc.customerID, tc.items)
			if err == nil {
				t.Fatalf("NewOrder() no devolvió error, pedido: %+v", o)
			}

			var domainErr *apperrors.DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("el error no es un DomainError: %v", err)
			}
			if domainErr.Type != apperrors.ErrorTypeValidation {
				t.Errorf("Type = %q, se esperaba %q", domainErr.Type, apperrors.ErrorTypeValidation)
			}
			if domainErr.Code != tc.wantCode {
				t.Errorf("Code = %q, se esperaba %q", domainErr.Code, tc.wantCode)
			}
		})
	}
}

// El total máximo posible (100 artículos con los valores máximos) debe caber en int64.
func TestNewOrder_MaxTotalDoesNotOverflow(t *testing.T) {
	items := make([]Item, MaxItems)
	for i := range items {
		items[i] = Item{SKU: "X" + strings.Repeat("1", i%3), Quantity: MaxQuantity, UnitPriceCents: MaxUnitPriceCents}
	}

	o, err := NewOrder("c1", items)
	if err != nil {
		t.Fatalf("NewOrder() devolvió error inesperado: %v", err)
	}
	want := int64(MaxItems) * int64(MaxQuantity) * int64(MaxUnitPriceCents)
	if o.TotalCents != want || o.TotalCents <= 0 {
		t.Errorf("TotalCents = %d, se esperaba %d", o.TotalCents, want)
	}
}
