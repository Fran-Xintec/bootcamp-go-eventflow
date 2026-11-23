package infrastructure

import (
	"context"
	"errors"
	"sync"
	"testing"

	"eventflow/internal/orders/application/dto"
	"eventflow/internal/orders/application/usecase"
	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/event"
	"eventflow/internal/orders/domain/repository"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, event.OrderCreated) error { return nil }

func newOrder(t *testing.T, customer string) *entity.Order {
	t.Helper()
	o, err := entity.NewOrder(customer, []entity.Item{{SKU: "A", Quantity: 1, UnitPriceCents: 100}})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestPostgresOrderRepository_SaveWithKeyAndFind(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo, _ := NewPostgresOrderRepository(db)

	order := newOrder(t, "cliente-clave")
	t.Cleanup(func() { cleanup(t, db, order.ID) })

	if err := repo.Save(ctx, order, &repository.Idempotency{Key: "test-clave-1", RequestHash: "hash-1"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	stored, err := repo.FindByIdempotencyKey(ctx, "test-clave-1")
	if err != nil {
		t.Fatalf("FindByIdempotencyKey: %v", err)
	}
	if stored.OrderID != order.ID || stored.RequestHash != "hash-1" {
		t.Errorf("clave guardada distinta: %+v", stored)
	}

	if _, err := repo.FindByIdempotencyKey(ctx, "no-existe"); !errors.Is(err, repository.ErrIdempotencyKeyNotFound) {
		t.Errorf("err = %v, se esperaba ErrIdempotencyKeyNotFound", err)
	}
}

// La segunda vez que se guarda la misma clave debe fallar y no dejar el segundo pedido.
func TestPostgresOrderRepository_DuplicateKeyRollsBack(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo, _ := NewPostgresOrderRepository(db)

	first, second := newOrder(t, "primero"), newOrder(t, "segundo")
	t.Cleanup(func() { cleanup(t, db, first.ID); cleanup(t, db, second.ID) })

	idem := &repository.Idempotency{Key: "test-clave-dup", RequestHash: "h"}
	if err := repo.Save(ctx, first, idem); err != nil {
		t.Fatalf("Save del primero: %v", err)
	}

	if err := repo.Save(ctx, second, idem); !errors.Is(err, repository.ErrIdempotencyKeyTaken) {
		t.Fatalf("err = %v, se esperaba ErrIdempotencyKeyTaken", err)
	}
	if _, err := repo.FindByID(ctx, second.ID); !errors.Is(err, repository.ErrOrderNotFound) {
		t.Errorf("el segundo pedido no debería haberse guardado, err = %v", err)
	}
}

// Simulación de reintentos simultáneos del cliente (RF-10): 20 peticiones iguales
// con la misma clave, contra la base real. Debe existir un solo pedido.
func TestCreateOrder_ConcurrentRetriesCreateOneOrder(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo, _ := NewPostgresOrderRepository(db)
	uc := usecase.NewCreateOrderUseCase(repo, noopPublisher{}, zap.NewNop())

	key := "test-clave-concurrente"
	req := dto.CreateOrderRequest{
		CustomerID: "cliente-concurrente",
		Items:      []dto.ItemRequest{{SKU: "A", Quantity: 2, UnitPriceCents: 500}},
	}

	const n = 20
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _, err := uc.Execute(ctx, req, &key)
			if err == nil {
				ids[i] = res.ID
			}
			errs[i] = err
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("la petición %d falló: %v", i, err)
		}
		if ids[i] != ids[0] {
			t.Fatalf("la petición %d devolvió otro pedido: %s y %s", i, ids[i], ids[0])
		}
	}
	t.Cleanup(func() { cleanup(t, db, ids[0]) })

	var count int
	pool := db.Native().(*pgxpool.Pool)
	err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys WHERE key = $1`, key).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("filas con la clave = %d (err %v), se esperaba 1", count, err)
	}
	err = pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE customer_id = 'cliente-concurrente'`).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("pedidos creados = %d (err %v), se esperaba 1", count, err)
	}
}
