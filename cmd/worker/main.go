// Worker: proceso independiente que consume el evento order.created, simula la
// notificación y confirma el pedido. Comparte módulo, configuración y base de datos
// con la API, pero se ejecuta (y se apaga) por separado.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"eventflow/cmd/config"
	notifInfra "eventflow/internal/notifications/infrastructure"
	"eventflow/internal/orders/application/usecase"
	orderInfra "eventflow/internal/orders/infrastructure"
	"eventflow/migrations"
	"eventflow/pkg/database"
	"eventflow/pkg/logger"
)

func main() {
	if err := run(); err != nil {
		log.Fatal("Error en el worker: ", err)
	}
}

func run() error {
	config.LoadEnv()

	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "dev"
	}
	logger.Init(env)
	lg := logger.L()
	defer lg.Sync() //nolint:errcheck // en la salida no se puede hacer nada con el error

	cfg, err := config.LoadVars()
	if err != nil {
		return fmt.Errorf("cargando configuración: %w", err)
	}

	ctx := context.Background()
	db, err := database.NewPostgresDatabase(ctx, database.PostgresConfig{
		Host: cfg.DBHost, Port: cfg.DBPort, User: cfg.DBUser,
		Password: cfg.DBPassword, DBName: cfg.DBName, SSLMode: cfg.DBSSLMode,
	})
	if err != nil {
		return fmt.Errorf("conectando a la base de datos: %w", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("la base de datos no responde: %w", err)
	}
	// Las migraciones son idempotentes y usan un candado, así que API y worker pueden correrlas a la vez.
	if err := database.RunMigrations(ctx, db, migrations.FS); err != nil {
		return fmt.Errorf("aplicando migraciones: %w", err)
	}

	repo, err := orderInfra.NewPostgresOrderRepository(db)
	if err != nil {
		return err
	}
	delay := time.Duration(cfg.NotifyDelayMs) * time.Millisecond
	processor := usecase.NewProcessOrderCreatedUseCase(repo, notifInfra.NewLogNotifier(delay, lg), lg)
	// Cada intento tiene un tope de tiempo propio, un poco mayor que la espera simulada.
	consumer := orderInfra.NewRabbitConsumer(cfg.RabbitMQURL, processor, lg, orderInfra.DefaultRetryDelays, delay+5*time.Second)

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	done := make(chan error, 1)
	go func() { done <- consumer.Run(sigCtx) }()

	select {
	case err := <-done: // el consumidor terminó solo (por ejemplo, se perdió la conexión)
		return err
	case <-sigCtx.Done():
	}

	// Apagado ordenado: se dejó de tomar mensajes y se espera el que está en curso.
	logger.General("Terminando worker...")
	select {
	case err := <-done:
		if err != nil {
			return err
		}
	case <-time.After(time.Duration(cfg.ShutDownTimeOut) * time.Second):
		return errors.New("venció el plazo de apagado con un mensaje sin terminar (volverá a la cola)")
	}

	logger.Success("Worker terminado correctamente")
	return nil
}
