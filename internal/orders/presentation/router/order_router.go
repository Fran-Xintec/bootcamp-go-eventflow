package router

import (
	"eventflow/internal/orders/presentation/handler"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registra las rutas tal como las fija el enunciado: /orders y /orders/{id}.
func RegisterRoutes(r gin.IRouter, h *handler.OrderHandler) {
	r.POST("/orders", h.Create)
	r.GET("/orders/:id", h.Get)
}
