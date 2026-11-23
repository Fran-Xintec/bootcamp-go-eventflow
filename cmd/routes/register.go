package routes

import (
	"eventflow/cmd/container"
	orders "eventflow/internal/orders/presentation/router"

	"github.com/gin-gonic/gin"
)

// ConfigureModule registra las rutas de cada módulo. Cuando haya otro módulo se agrega aquí.
func ConfigureModule(router *gin.Engine, c *container.Container) {
	orders.RegisterRoutes(router, c.OrderHandler)
}
