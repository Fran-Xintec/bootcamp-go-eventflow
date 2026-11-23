package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Número cualquiera para el candado de la base: evita que dos instancias
// de la API apliquen las migraciones al mismo tiempo.
const migrationLockID = 727274

// RunMigrations aplica, en orden, los archivos *.up.sql que todavía no se
// han aplicado y guarda su nombre en la tabla schema_migrations.
// Todo ocurre en una sola transacción, así que o se aplica todo o nada.
func RunMigrations(ctx context.Context, db Database, files fs.FS) error {
	pool, ok := db.Native().(*pgxpool.Pool)
	if !ok {
		return errors.New("la base de datos no es un pool de pgx")
	}

	names, err := fs.Glob(files, "*.up.sql")
	if err != nil {
		return fmt.Errorf("listando migraciones: %w", err)
	}
	sort.Strings(names) // el prefijo numérico (001_, 002_...) define el orden

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("iniciando transacción de migraciones: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // tras el commit devuelve error y no importa

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("tomando el candado de migraciones: %w", err)
	}

	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("creando schema_migrations: %w", err)
	}

	for _, name := range names {
		var applied bool
		err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", name).Scan(&applied)
		if err != nil {
			return fmt.Errorf("consultando migración %s: %w", name, err)
		}
		if applied {
			continue
		}

		sql, err := fs.ReadFile(files, name)
		if err != nil {
			return fmt.Errorf("leyendo migración %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("aplicando migración %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
			return fmt.Errorf("registrando migración %s: %w", name, err)
		}
	}

	return tx.Commit(ctx)
}
