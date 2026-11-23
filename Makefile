# Atajos para el día a día. En Windows sin make, los comandos de cada objetivo se pueden
# copiar tal cual (también están en el README).
.PHONY: help up down run test test-integration test-race cover vet

help: ## Muestra los comandos disponibles
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

up: ## Levanta PostgreSQL, RabbitMQ, la API y el worker con Docker
	docker compose up --build

down: ## Apaga todo (agregar -v borra también los datos)
	docker compose down

run: ## Corre la API en local (necesita la base: docker compose up -d db rabbitmq)
	go run ./cmd/api

test: ## Pruebas unitarias y HTTP (no necesitan base de datos)
	go test ./...

test-integration: ## Incluye las pruebas con PostgreSQL y RabbitMQ (docker compose up -d db rabbitmq antes)
	DB_HOST=localhost RABBITMQ_URL=amqp://guest:guest@localhost:5672/ go test -count=1 ./...

test-race: ## Pruebas con detector de carreras, dentro de Docker (ver README)
	docker run --rm -v "$(CURDIR)":/app -w /app golang:1.25 go test -race ./...

cover: ## Cobertura de las pruebas
	go test -cover ./...

vet: ## Revisión estática
	go vet ./...
