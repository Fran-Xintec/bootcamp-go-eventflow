package config

import (
	"os"

	"github.com/joho/godotenv"
)

// LoadEnv carga el archivo .env si existe (solo para desarrollo local).
// Si no existe no pasa nada: se usan las variables que ya tenga el sistema,
// que es lo que ocurre dentro de Docker.
func LoadEnv() {
	if _, err := os.Stat(".env"); err == nil {
		// Las variables ya definidas en el sistema tienen prioridad sobre el archivo.
		_ = godotenv.Load(".env")
	}
}
