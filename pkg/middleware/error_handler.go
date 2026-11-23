package middleware

import (
	"eventflow/pkg/apperrors"
	"eventflow/pkg/httputil"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ErrorHandlerMiddleware maneja errores globalmente
func ErrorHandlerMiddleware(log *zap.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		var err error

		switch e := recovered.(type) {
		case *apperrors.DomainError:
			err = e
		case error:
			err = e
		default:
			err = apperrors.NewInternalError("PANIC_001", "Ocurrió un error inesperado")
		}

		// Mapear error a HTTP
		problem := httputil.MapErrorToHttp(err)
		statusCode := problem.Status

		// Log según severidad
		if statusCode >= 500 {
			log.Error("Error del servidor",
				zap.String("error", err.Error()),
				zap.String("path", c.Request.URL.Path),
				zap.String("method", c.Request.Method),
				zap.Int("status", statusCode),
			)
		} else {
			log.Warn("Error del cliente",
				zap.String("error", err.Error()),
				zap.String("path", c.Request.URL.Path),
				zap.String("method", c.Request.Method),
				zap.Int("status", statusCode),
			)
		}

		httputil.WriteProblem(c, problem)
	})
}
