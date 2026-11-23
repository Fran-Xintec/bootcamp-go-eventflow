package middleware

import (
	"eventflow/pkg/requestid"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequestID asigna a cada petición un identificador de correlación: el que envía el
// cliente en X-Request-Id (si es válido) o uno nuevo. Se devuelve en la respuesta y
// queda en el context para los logs, los errores y el evento del pedido.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestid.Header)
		if !requestid.Valid(id) {
			id = uuid.NewString()
		}

		c.Header(requestid.Header, id)
		c.Request = c.Request.WithContext(requestid.With(c.Request.Context(), id))
		c.Next()
	}
}
