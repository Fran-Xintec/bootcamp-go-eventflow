package config

import (
	"github.com/caarlos0/env/v6"
	"github.com/go-playground/validator/v10"
)

// Configuration agrupa todo lo que la aplicación lee del entorno.
// Los valores por defecto sirven para desarrollo local; los de producción
// se entregan siempre por variables de entorno (ver .env.example).
type Configuration struct {
	AppEnv          string `env:"ENVIRONMENT" envDefault:"dev" validate:"oneof=dev qa prod"`
	Port            int    `env:"PORT" envDefault:"8080"`
	ShutDownTimeOut int    `env:"SHUTDOWN_TIMEOUT" envDefault:"10"`

	DBHost     string `env:"DB_HOST" envDefault:"localhost"`
	DBPort     int    `env:"DB_PORT" envDefault:"5432"`
	DBUser     string `env:"DB_USER" envDefault:"eventflow"`
	DBPassword string `env:"DB_PASSWORD" envDefault:"eventflow"`
	DBName     string `env:"DB_NAME" envDefault:"eventflow"`
	DBSSLMode  string `env:"DB_SSL_MODE" envDefault:"disable"`

	// Plazo máximo (en milisegundos) para atender una petición, base de datos incluida.
	RequestTimeoutMs int `env:"REQUEST_TIMEOUT_MS" envDefault:"5000"`

	// Dirección de RabbitMQ. El valor por defecto es el usuario de ejemplo del broker local.
	RabbitMQURL string `env:"RABBITMQ_URL" envDefault:"amqp://guest:guest@localhost:5672/"`

	// Espera (en milisegundos) de la notificación simulada (la hace el worker).
	NotifyDelayMs int `env:"NOTIFY_DELAY_MS" envDefault:"200"`
}

func LoadVars() (*Configuration, error) {
	cfg := &Configuration{}

	if err := env.Parse(cfg); err != nil {
		return nil, err
	}

	v := validator.New()

	if err := v.Struct(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
