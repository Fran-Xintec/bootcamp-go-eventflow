package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"eventflow/internal/orders/application/dto"
	"eventflow/internal/orders/application/usecase"
	"eventflow/pkg/apperrors"
	"eventflow/pkg/httputil"
	"eventflow/pkg/requestid"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Tamaño máximo del cuerpo de POST /orders (1 MiB). Un pedido normal pesa unos pocos KB.
const maxBodyBytes = 1 << 20

// OrderHandler solo traduce HTTP a casos de uso y de vuelta.
// Ninguna regla de negocio vive aquí.
type OrderHandler struct {
	create         *usecase.CreateOrderUseCase
	get            *usecase.GetOrderUseCase
	log            *zap.Logger
	requestTimeout time.Duration
}

func NewOrderHandler(create *usecase.CreateOrderUseCase, get *usecase.GetOrderUseCase, log *zap.Logger, requestTimeout time.Duration) *OrderHandler {
	return &OrderHandler{create: create, get: get, log: log, requestTimeout: requestTimeout}
}

// requestContext acota el trabajo de la petición: si la base tarda más del plazo,
// la consulta se cancela y la respuesta es 503 en vez de quedar esperando.
func (h *OrderHandler) requestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), h.requestTimeout)
}

// Create atiende POST /orders. Si la petición repite una Idempotency-Key ya usada con
// el mismo cuerpo, responde 200 con el pedido original y Idempotent-Replayed: true.
func (h *OrderHandler) Create(c *gin.Context) {
	// El cuerpo debe declararse como JSON (si no, 415).
	if c.ContentType() != "application/json" {
		h.fail(c, apperrors.NewUnsupportedMediaTypeError("UNSUPPORTED_MEDIA_TYPE", "el Content-Type debe ser application/json"))
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)

	var req dto.CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.fail(c, apperrors.NewPayloadTooLargeError("REQUEST_TOO_LARGE", "el cuerpo supera el máximo de 1 MiB"))
			return
		}
		h.fail(c, apperrors.NewBadRequestError("INVALID_JSON", "el cuerpo de la petición no es un JSON válido"))
		return
	}

	// La cabecera es opcional: nil significa "no vino", que no es lo mismo que vacía.
	var idempotencyKey *string
	if values := c.Request.Header.Values("Idempotency-Key"); len(values) > 0 {
		idempotencyKey = &values[0]
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()

	res, replayed, err := h.create.Execute(ctx, req, idempotencyKey)
	if err != nil {
		h.fail(c, err)
		return
	}

	// Un reintento no crea nada: se responde 200 con el pedido original. Solo la
	// petición que realmente lo crea responde 201 con su Location.
	if replayed {
		c.Header("Idempotent-Replayed", "true")
		c.JSON(http.StatusOK, res)
		return
	}
	c.Header("Location", "/orders/"+res.ID)
	c.JSON(http.StatusCreated, res)
}

// Get atiende GET /orders/:id.
func (h *OrderHandler) Get(c *gin.Context) {
	ctx, cancel := h.requestContext(c)
	defer cancel()

	res, err := h.get.Execute(ctx, c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, res)
}

// fail responde con el formato de error común. Los errores inesperados (5xx)
// se registran con su detalle real, pero al cliente solo le llega el mensaje genérico.
func (h *OrderHandler) fail(c *gin.Context, err error) {
	problem := httputil.MapErrorToHttp(err)
	if problem.Status >= http.StatusInternalServerError {
		h.log.Error("error al atender la petición",
			zap.String("request_id", requestid.From(c.Request.Context())),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Error(err),
		)
	}
	httputil.WriteProblem(c, problem)
}
