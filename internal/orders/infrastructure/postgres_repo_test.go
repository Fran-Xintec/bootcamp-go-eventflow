package infrastructure

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"

	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/repository"
	"eventflow/migrations"
	"eventflow/pkg/database"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestDB se conecta a la base del docker compose. Si no hay DB_HOST
// definido la prueba se salta, así `go test ./...` funciona sin Docker.
func newTestDB(t *testing.T) database.Database {
	t.Helper()

	host := os.Getenv("DB_HOST")
	if host == "" {
		t.Skip("DB_HOST no definido: se salta la prueba de integración (levantar `docker compose up -d db`)")
	}
	port, _ := strconv.Atoi(getenv("DB_PORT", "5432"))

	db, err := database.NewPostgresDatabase(context.Background(), database.PostgresConfig{
		Host:     host,
		Port:     port,
		User:     getenv("DB_USER", "eventflow"),
		Password: getenv("DB_PASSWORD", "eventflow"),
		DBName:   getenv("DB_NAME", "eventflow"),
		SSLMode:  getenv("DB_SSL_MODE", "disable"),
	})
	if err != nil {
		t.Fatalf("no se pudo crear el pool: %v", err)
	}
	t.Cleanup(db.Close)

	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("la base no responde: %v", err)
	}
	if err := database.RunMigrations(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("migraciones: %v", err)
	}
	return db
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func cleanup(t *testing.T, db database.Database, orderID string) {
	t.Helper()
	pool := db.Native().(*pgxpool.Pool)
	if _, err := pool.Exec(context.Background(), "DELETE FROM orders WHERE id = $1", orderID); err != nil {
		t.Errorf("limpiando el pedido de prueba: %v", err)
	}
}

func TestMigrations_CanRunTwice(t *testing.T) {
	db := newTestDB(t) // ya aplicó las migraciones una vez

	if err := database.RunMigrations(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("la segunda ejecución de las migraciones falló: %v", err)
	}
}

func TestPostgresOrderRepository_SaveAndFind(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo, err := NewPostgresOrderRepository(db)
	if err != nil {
		t.Fatalf("NewPostgresOrderRepository: %v", err)
	}

	order, err := entity.NewOrder("cliente-integracion", []entity.Item{
		{SKU: "A-1", Quantity: 2, UnitPriceCents: 1500},
		{SKU: "B-2", Quantity: 1, UnitPriceCents: 999},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Se limpia lo que se crea para no dejar datos de prueba en la base.
	t.Cleanup(func() { cleanup(t, db, order.ID) })

	if err := repo.Save(ctx, order, nil); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.FindByID(ctx, order.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.ID != order.ID || got.CustomerID != order.CustomerID || got.Status != entity.StatusCreated || got.TotalCents != 3999 {
		t.Errorf("pedido leído distinto al guardado: %+v", got)
	}
	if len(got.Items) != 2 || got.Items[0].SKU != "A-1" || got.Items[1].SKU != "B-2" {
		t.Errorf("artículos (o su orden) distintos: %+v", got.Items)
	}
}

func TestPostgresOrderRepository_NotFound(t *testing.T) {
	db := newTestDB(t)
	repo, _ := NewPostgresOrderRepository(db)

	_, err := repo.FindByID(context.Background(), "6f1b0f7e-8f5c-4c37-9c63-1f0d9d0a1111")
	if !errors.Is(err, repository.ErrOrderNotFound) {
		t.Fatalf("err = %v, se esperaba ErrOrderNotFound", err)
	}
}

// Si un artículo falla, el pedido tampoco debe quedar guardado (transacción).
func TestPostgresOrderRepository_SaveIsAtomic(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo, _ := NewPostgresOrderRepository(db)

	order, _ := entity.NewOrder("cliente-atomico", []entity.Item{{SKU: "A", Quantity: 1, UnitPriceCents: 100}})
	t.Cleanup(func() { cleanup(t, db, order.ID) })

	// Se fuerza un fallo en el artículo: la base rechaza cantidad <= 0 (CHECK).
	order.Items[0].Quantity = 0

	if err := repo.Save(ctx, order, nil); err == nil {
		t.Fatal("se esperaba un error al guardar un artículo inválido")
	}
	if _, err := repo.FindByID(ctx, order.ID); !errors.Is(err, repository.ErrOrderNotFound) {
		t.Errorf("el pedido no debería haber quedado guardado, err = %v", err)
	}
}
