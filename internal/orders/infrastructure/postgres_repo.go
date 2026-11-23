package infrastructure

import (
	"context"
	"errors"
	"fmt"

	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/repository"
	"eventflow/pkg/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresOrderRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresOrderRepository(db database.Database) (*PostgresOrderRepository, error) {
	pool, ok := db.Native().(*pgxpool.Pool)
	if !ok {
		return nil, errors.New("la base de datos no es un pool de pgx")
	}
	return &PostgresOrderRepository{pool: pool}, nil
}

// Código de PostgreSQL para "violación de restricción única".
const uniqueViolation = "23505"

// Save guarda el pedido, sus artículos y (si viene) la clave de idempotencia en
// una sola transacción: si falla algo a la mitad no queda un pedido a medias.
// Si dos peticiones con la misma clave llegan a la vez, la restricción única de
// la clave deja pasar solo a una; la otra recibe ErrIdempotencyKeyTaken.
func (r *PostgresOrderRepository) Save(ctx context.Context, o *entity.Order, idem *repository.Idempotency) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("iniciando transacción: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // tras el commit devuelve error y no importa

	_, err = tx.Exec(ctx,
		`INSERT INTO orders (id, customer_id, status, total_cents) VALUES ($1, $2, $3, $4)`,
		o.ID, o.CustomerID, string(o.Status), o.TotalCents)
	if err != nil {
		return fmt.Errorf("insertando pedido %s: %w", o.ID, err)
	}

	for i, it := range o.Items {
		_, err = tx.Exec(ctx,
			`INSERT INTO order_items (order_id, position, sku, quantity, unit_price_cents) VALUES ($1, $2, $3, $4, $5)`,
			o.ID, i, it.SKU, it.Quantity, it.UnitPriceCents)
		if err != nil {
			return fmt.Errorf("insertando artículo %d del pedido %s: %w", i, o.ID, err)
		}
	}

	if idem != nil {
		_, err = tx.Exec(ctx,
			`INSERT INTO idempotency_keys (key, request_hash, order_id) VALUES ($1, $2, $3)`,
			idem.Key, idem.RequestHash, o.ID)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return repository.ErrIdempotencyKeyTaken
		}
		if err != nil {
			return fmt.Errorf("guardando la clave de idempotencia del pedido %s: %w", o.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("confirmando pedido %s: %w", o.ID, err)
	}
	return nil
}

// FindByIdempotencyKey devuelve repository.ErrIdempotencyKeyNotFound si la clave no existe.
func (r *PostgresOrderRepository) FindByIdempotencyKey(ctx context.Context, key string) (*repository.StoredKey, error) {
	stored := &repository.StoredKey{}
	err := r.pool.QueryRow(ctx,
		`SELECT request_hash, order_id FROM idempotency_keys WHERE key = $1`, key,
	).Scan(&stored.RequestHash, &stored.OrderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrIdempotencyKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("buscando la clave de idempotencia: %w", err)
	}
	return stored, nil
}

// IsEventProcessed indica si el evento ya fue registrado por el worker.
func (r *PostgresOrderRepository) IsEventProcessed(ctx context.Context, eventID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM processed_events WHERE event_id = $1)`, eventID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("consultando el evento %s: %w", eventID, err)
	}
	return exists, nil
}

// ConfirmOrder registra el evento y confirma el pedido en una sola transacción.
// Si el evento ya estaba registrado no hace nada y devuelve false.
func (r *PostgresOrderRepository) ConfirmOrder(ctx context.Context, eventID, orderID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("iniciando transacción: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // tras el commit devuelve error y no importa

	tag, err := tx.Exec(ctx, `INSERT INTO processed_events (event_id) VALUES ($1) ON CONFLICT DO NOTHING`, eventID)
	if err != nil {
		return false, fmt.Errorf("registrando el evento %s: %w", eventID, err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	tag, err = tx.Exec(ctx, `UPDATE orders SET status = $1 WHERE id = $2`, string(entity.StatusConfirmed), orderID)
	if err != nil {
		return false, fmt.Errorf("confirmando el pedido %s: %w", orderID, err)
	}
	if tag.RowsAffected() == 0 {
		return false, repository.ErrOrderNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("confirmando la transacción del evento %s: %w", eventID, err)
	}
	return true, nil
}

// FindByID devuelve repository.ErrOrderNotFound si el pedido no existe.
func (r *PostgresOrderRepository) FindByID(ctx context.Context, id string) (*entity.Order, error) {
	o := &entity.Order{}
	var status string

	err := r.pool.QueryRow(ctx,
		`SELECT id, customer_id, status, total_cents FROM orders WHERE id = $1`, id,
	).Scan(&o.ID, &o.CustomerID, &status, &o.TotalCents)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("buscando pedido %s: %w", id, err)
	}
	o.Status = entity.Status(status)

	rows, err := r.pool.Query(ctx,
		`SELECT sku, quantity, unit_price_cents FROM order_items WHERE order_id = $1 ORDER BY position`, id)
	if err != nil {
		return nil, fmt.Errorf("buscando artículos del pedido %s: %w", id, err)
	}
	defer rows.Close()

	for rows.Next() {
		var it entity.Item
		if err := rows.Scan(&it.SKU, &it.Quantity, &it.UnitPriceCents); err != nil {
			return nil, fmt.Errorf("leyendo artículo del pedido %s: %w", id, err)
		}
		o.Items = append(o.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorriendo artículos del pedido %s: %w", id, err)
	}

	return o, nil
}
