package middleware

import (
	"strings"
	"time"

	"eventflow/pkg/requestid"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// AccessLog escribe un log estructurado por petición: id de correlación, método,
// ruta, estado y duración. No registra cuerpos ni cabeceras, porque pueden traer
// datos del cliente. Las rutas operativas (health y métricas) se omiten para no llenar el log.
func AccessLog(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/health") || path == "/metrics" {
			return
		}

		log.Info("petición atendida",
			zap.String("request_id", requestid.From(c.Request.Context())),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	}
}
