package app

import (
	"context"
	"fmt"
	"os"

	"eventflow/cmd/config"
	"eventflow/cmd/container"
	"eventflow/cmd/routes"
	"eventflow/migrations"
	"eventflow/pkg/database"
	"eventflow/pkg/logger"
	"eventflow/pkg/middleware"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type App struct {
	config    *config.Configuration
	container *container.Container
	router    *gin.Engine
	log       *zap.Logger
	db        database.Database
}

func NewApp() *App {
	return &App{}
}

// Init deja todo listo para arrancar: configuración, logger, base de datos,
// migraciones y rutas. Si algo falla devuelve el error con contexto.
func (a *App) Init() error {
	config.LoadEnv()

	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "dev"
	}

	logger.Init(env)
	a.log = logger.L()
	logger.General("==== EventFlow iniciando ====")

	cfg, err := config.LoadVars()
	if err != nil {
		return fmt.Errorf("cargando configuración: %w", err)
	}
	a.config = cfg
	logger.Success("Configuración cargada correctamente")

	if cfg.AppEnv == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(middleware.RequestID())                   // id de correlación (primero, para que lo vean los demás)
	router.Use(middleware.ErrorHandlerMiddleware(a.log)) // recupera panics y responde con el formato común
	router.Use(middleware.AccessLog(a.log))              // un log estructurado por petición
	router.Use(middleware.Metrics())                     // contador y duración de peticiones
	router.Use(middleware.SecurityHeaders())

	ctx := context.Background()

	db, err := database.NewPostgresDatabase(ctx, database.PostgresConfig{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		DBName:   cfg.DBName,
		Password: cfg.DBPassword,
		SSLMode:  cfg.DBSSLMode,
	})
	if err != nil {
		return fmt.Errorf("conectando a la base de datos: %w", err)
	}
	a.db = db

	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("la base de datos no responde: %w", err)
	}

	if err := database.RunMigrations(ctx, db, migrations.FS); err != nil {
		return fmt.Errorf("aplicando migraciones: %w", err)
	}
	logger.Success("Migraciones al día")

	c, err := container.NewContainer(cfg, db, a.log)
	if err != nil {
		return fmt.Errorf("armando dependencias: %w", err)
	}
	a.container = c

	routes.NewAPIRouter(c).RegisterRoutes(router)
	a.router = router

	return nil
}
