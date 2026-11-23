package routes

import (
	"net/http"
	"strings"

	"eventflow/cmd/container"
	"eventflow/pkg/httputil"
	"eventflow/pkg/logger"

	"github.com/gin-gonic/gin"
)

type APIRouter struct {
	container *container.Container
}

func NewAPIRouter(cont *container.Container) *APIRouter {
	return &APIRouter{
		container: cont,
	}
}

func (r *APIRouter) RegisterRoutes(router *gin.Engine) {
	ConfigureModule(router, r.container)
	registerOps(router, r.container.DB, r.container.Log)
	registerFallbacks(router)
	logger.Success("Ruta API Registradas correctamente")
}

// registerFallbacks hace que una ruta o un método inexistente respondan con el
// mismo formato de error (RFC 9457) que el resto de la API; Gin responde texto plano por defecto.
func registerFallbacks(router *gin.Engine) {
	router.HandleMethodNotAllowed = true

	router.NoRoute(func(c *gin.Context) {
		httputil.WriteProblem(c, httputil.NewProblem(http.StatusNotFound, "ROUTE_NOT_FOUND", "La ruta no existe"))
	})
	router.NoMethod(func(c *gin.Context) {
		// RFC 9110 (15.5.6): un 405 debe indicar los métodos permitidos.
		c.Header("Allow", allowedMethods(router, c.Request.URL.Path))
		httputil.WriteProblem(c, httputil.NewProblem(http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Método no permitido para esta ruta"))
	})
}

// allowedMethods lista los métodos registrados para una ruta, comparando
// segmento a segmento (un segmento ":id" acepta cualquier valor).
func allowedMethods(router *gin.Engine, path string) string {
	var methods []string
	for _, route := range router.Routes() {
		if samePath(route.Path, path) {
			methods = append(methods, route.Method)
		}
	}
	return strings.Join(methods, ", ")
}

func samePath(pattern, path string) bool {
	p, q := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(p) != len(q) {
		return false
	}
	for i := range p {
		if !strings.HasPrefix(p[i], ":") && p[i] != q[i] {
			return false
		}
	}
	return true
}
