package usecase

import (
	"context"

	"eventflow/internal/orders/application/dto"
	"eventflow/internal/orders/domain/repository"
	"eventflow/pkg/apperrors"

	"github.com/google/uuid"
)

type GetOrderUseCase struct {
	repo repository.OrderRepository
}

func NewGetOrderUseCase(repo repository.OrderRepository) *GetOrderUseCase {
	return &GetOrderUseCase{repo: repo}
}

func (uc *GetOrderUseCase) Execute(ctx context.Context, id string) (*dto.OrderResponse, error) {
	// Si el id no es un UUID ni siquiera vale la pena ir a la base.
	if _, err := uuid.Parse(id); err != nil {
		return nil, apperrors.NewBadRequestError("ORDER_ID_INVALID", "el id del pedido debe ser un UUID válido")
	}

	order, err := uc.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return dto.NewOrderResponse(order), nil
}
