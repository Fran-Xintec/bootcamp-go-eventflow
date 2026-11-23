package container

import (
	"time"

	"eventflow/cmd/config"
	orderUseCase "eventflow/internal/orders/application/usecase"
	orderInfra "eventflow/internal/orders/infrastructure"
	"eventflow/internal/orders/presentation/handler"
	"eventflow/pkg/database"

	"go.uber.org/zap"
)

// Container es la raíz de composición: aquí (y solo aquí) se arman las
// implementaciones concretas y se inyectan en los casos de uso.
type Container struct {
	DB           database.Database
	OrderHandler *handler.OrderHandler
	Publisher    *orderInfra.RabbitPublisher
	Log          *zap.Logger
}

func NewContainer(cfg *config.Configuration, db database.Database, log *zap.Logger) (*Container, error) {
	repo, err := orderInfra.NewPostgresOrderRepository(db)
	if err != nil {
		return nil, err
	}

	// El evento se publica en RabbitMQ; la notificación la hace el worker.
	publisher := orderInfra.NewRabbitPublisher(cfg.RabbitMQURL, log)
	createUC := orderUseCase.NewCreateOrderUseCase(repo, publisher, log)
	getUC := orderUseCase.NewGetOrderUseCase(repo)

	return &Container{
		DB:           db,
		OrderHandler: handler.NewOrderHandler(createUC, getUC, log, time.Duration(cfg.RequestTimeoutMs)*time.Millisecond),
		Publisher:    publisher,
		Log:          log,
	}, nil
}
