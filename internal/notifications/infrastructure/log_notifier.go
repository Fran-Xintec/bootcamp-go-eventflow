package infrastructure

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"eventflow/internal/orders/domain/entity"
	"eventflow/pkg/requestid"

	"go.uber.org/zap"
)

// Clientes de demostración: hacen fallar a la notificación simulada para poder
// mostrar los reintentos y la cola de fallidos (solo es una simulación).
const (
	demoPermanentFailure = "demo-fallo-permanente"  // falla siempre
	demoTransientFailure = "demo-fallo-transitorio" // falla en los 2 primeros intentos de cada pedido
	transientFailures    = 2
)

// LogNotifier simula el envío de una notificación: espera un rato y escribe en el log.
// No envía nada real (el enunciado deja fuera el correo real).
type LogNotifier struct {
	delay time.Duration
	log   *zap.Logger

	mu       sync.Mutex
	attempts map[string]int // intentos por pedido, solo para el fallo transitorio
}

func NewLogNotifier(delay time.Duration, log *zap.Logger) *LogNotifier {
	return &LogNotifier{delay: delay, log: log, attempts: map[string]int{}}
}

func (n *LogNotifier) Notify(ctx context.Context, order *entity.Order) error {
	if err := n.simulatedFailure(order); err != nil {
		return err
	}

	select {
	case <-time.After(n.delay):
	case <-ctx.Done():
		return ctx.Err()
	}

	n.log.Info("notificación simulada enviada", zap.String("request_id", requestid.From(ctx)), zap.String("order_id", order.ID))
	return nil
}

func (n *LogNotifier) simulatedFailure(order *entity.Order) error {
	switch {
	case strings.HasPrefix(order.CustomerID, demoPermanentFailure):
		return errors.New("fallo simulado permanente del servicio de notificaciones")
	case strings.HasPrefix(order.CustomerID, demoTransientFailure):
		n.mu.Lock()
		defer n.mu.Unlock()
		n.attempts[order.ID]++
		if n.attempts[order.ID] <= transientFailures {
			return errors.New("fallo simulado transitorio del servicio de notificaciones")
		}
	}
	return nil
}
