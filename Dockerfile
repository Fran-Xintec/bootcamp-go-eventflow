# Etapa 1: compilar
FROM golang:1.25-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/eventflow ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/worker ./cmd/worker

# Etapa 2: imagen final mínima, sin shell y con usuario sin privilegios
FROM gcr.io/distroless/static-debian12:nonroot

# Una sola imagen con los dos procesos; el worker se arranca con otro entrypoint (ver docker-compose.yml).
COPY --from=builder /app/eventflow /eventflow
COPY --from=builder /app/worker /worker

EXPOSE 8080
ENTRYPOINT ["/eventflow"]
