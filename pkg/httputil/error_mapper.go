package httputil

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"eventflow/pkg/apperrors"
	"eventflow/pkg/requestid"

	"github.com/gin-gonic/gin"
)

// Problem es la forma común de todos los errores de la API, según RFC 9457
// (Problem Details for HTTP APIs), con content-type application/problem+json:
//
//	{"type": "/problems/ORDER_NOT_FOUND", "title": "Not Found", "status": 404,
//	 "detail": "Pedido no encontrado", "instance": "/orders/...", "code": "ORDER_NOT_FOUND"}
//
// "code" es una extensión permitida por el RFC: un identificador estable para programas.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
	// RequestID es el identificador de correlación de la petición (extensión),
	// para que quien reporta el error y quien lo investiga hablen de lo mismo.
	RequestID string `json:"request_id,omitempty"`
}

// NewProblem arma el cuerpo de un error. "title" es la frase estándar del código HTTP
// y "detail" el mensaje en español para quien lo lee.
func NewProblem(status int, code, detail string) Problem {
	return Problem{
		Type:   "/problems/" + code,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	}
}

// WriteProblem responde el error con el content-type que define el RFC 9457.
func WriteProblem(c *gin.Context, p Problem) {
	p.Instance = c.Request.URL.Path
	p.RequestID = requestid.From(c.Request.Context())
	body, err := json.Marshal(p)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(p.Status, "application/problem+json", body)
}

// MapErrorToHttp convierte un error en su Problem (que incluye el código HTTP).
// Usa errors.As para reconocer errores de dominio aunque vengan envueltos con %w.
// Cualquier otro error responde 500 con un mensaje genérico: el detalle real
// (SQL, rutas, credenciales) nunca sale en la respuesta, solo queda en el log.
func MapErrorToHttp(err error) Problem {
	var domainErr *apperrors.DomainError
	if errors.As(err, &domainErr) {
		return NewProblem(statusFor(domainErr.Type), domainErr.Code, domainErr.Message)
	}

	// Se agotó el plazo de la petición (por ejemplo, una consulta lenta a la base).
	if errors.Is(err, context.DeadlineExceeded) {
		return NewProblem(http.StatusServiceUnavailable, "REQUEST_TIMEOUT",
			"La operación tardó más de lo permitido, se puede reintentar")
	}

	return NewProblem(http.StatusInternalServerError, "INTERNAL_001", "Error interno del servidor")
}

func statusFor(errorType apperrors.ErrorType) int {
	switch errorType {
	case apperrors.ErrorTypeBadRequest:
		return http.StatusBadRequest
	case apperrors.ErrorTypeValidation:
		return http.StatusUnprocessableEntity
	case apperrors.ErrorTypeUnsupported:
		return http.StatusUnsupportedMediaType
	case apperrors.ErrorTypeTooLarge:
		return http.StatusRequestEntityTooLarge
	case apperrors.ErrorTypeUnavailable:
		return http.StatusServiceUnavailable
	case apperrors.ErrorTypeNotFound:
		return http.StatusNotFound
	case apperrors.ErrorTypeConflict:
		return http.StatusConflict
	case apperrors.ErrorTypeUnauthorized:
		return http.StatusUnauthorized
	case apperrors.ErrorTypeForbidden:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
