package routes

import (
	"context"
	"net/http"
	"time"

	"eventflow/pkg/httputil"
	"eventflow/pkg/middleware"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// pinger es lo único que el readiness necesita de la base de datos.
type pinger interface {
	Ping(ctx context.Context) error
}

// registerOps registra las rutas operativas: health y métricas.
//   - /health/live: el proceso está vivo (no consulta nada).
//   - /health/ready: la base de datos responde. RabbitMQ no se incluye a propósito:
//     la API sigue siendo útil sin el broker (el pedido se guarda y se registra el error).
//   - /metrics: métricas en formato Prometheus.
//
// Quedan sin autenticación, como el resto de la API; en un despliegue real se
// restringirían a la red interna.
func registerOps(router *gin.Engine, db pinger, log *zap.Logger) {
	router.GET("/health/live", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/health/ready", readyHandler(db, log))
	router.GET("/metrics", middleware.MetricsHandler())
}

func readyHandler(db pinger, log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			log.Error("el readiness falló: la base de datos no responde", zap.Error(err))
			httputil.WriteProblem(c, httputil.NewProblem(http.StatusServiceUnavailable, "NOT_READY", "La base de datos no responde"))
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "checks": gin.H{"database": "ok"}})
	}
}
