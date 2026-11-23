package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total de peticiones HTTP atendidas.",
	}, []string{"method", "path", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Duración de las peticiones HTTP en segundos.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

// Metrics cuenta las peticiones y mide su duración. Usa la ruta registrada
// (/orders/:id) y no la URL real, para no crear una serie por cada pedido.
func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "no_encontrada"
		}
		httpRequests.WithLabelValues(c.Request.Method, path, strconv.Itoa(c.Writer.Status())).Inc()
		httpDuration.WithLabelValues(c.Request.Method, path).Observe(time.Since(start).Seconds())
	}
}

// MetricsHandler expone las métricas en formato Prometheus (GET /metrics).
func MetricsHandler() gin.HandlerFunc {
	return gin.WrapH(promhttp.Handler())
}
