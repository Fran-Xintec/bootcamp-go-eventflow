package routes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"eventflow/pkg/middleware"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

func opsServer(db pinger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID(), middleware.Metrics())
	registerOps(r, db, zap.NewNop())
	registerFallbacks(r)
	return r
}

func get(r http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestHealth_LiveAndReady(t *testing.T) {
	r := opsServer(fakePinger{})

	for _, path := range []string{"/health/live", "/health/ready"} {
		if rec := get(r, path, nil); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, se esperaba 200", path, rec.Code)
		}
	}
}

func TestHealth_ReadyFailsWhenDatabaseIsDown(t *testing.T) {
	r := opsServer(fakePinger{err: errors.New("connection refused")})

	ready := get(r, "/health/ready", nil)
	if ready.Code != http.StatusServiceUnavailable || !strings.Contains(ready.Body.String(), "NOT_READY") {
		t.Errorf("ready: status = %d, cuerpo = %s; se esperaba 503 NOT_READY", ready.Code, ready.Body)
	}
	if strings.Contains(ready.Body.String(), "connection refused") {
		t.Error("el error de la base no debe salir en la respuesta")
	}
	// El proceso sigue vivo aunque la base esté caída.
	if live := get(r, "/health/live", nil); live.Code != http.StatusOK {
		t.Errorf("live: status = %d, se esperaba 200", live.Code)
	}
}

func TestMetrics_ExposesHTTPMetrics(t *testing.T) {
	r := opsServer(fakePinger{})
	get(r, "/health/live", nil) // genera al menos una petición medida

	rec := get(r, "/metrics", nil)

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "http_requests_total") || !strings.Contains(body, "http_request_duration_seconds") {
		t.Errorf("las métricas no incluyen lo esperado (status %d)", rec.Code)
	}
}

func TestRequestID_EchoedAndGenerated(t *testing.T) {
	r := opsServer(fakePinger{})

	if got := get(r, "/health/live", map[string]string{"X-Request-Id": "demo-123"}).Header().Get("X-Request-Id"); got != "demo-123" {
		t.Errorf("X-Request-Id = %q, se esperaba el que envió el cliente", got)
	}
	if got := get(r, "/health/live", nil).Header().Get("X-Request-Id"); len(got) != 36 {
		t.Errorf("X-Request-Id = %q, se esperaba un UUID generado", got)
	}
	// Un id inválido (con espacios) se descarta y se genera otro.
	if got := get(r, "/health/live", map[string]string{"X-Request-Id": "tiene espacios"}).Header().Get("X-Request-Id"); got == "tiene espacios" {
		t.Error("un id inválido no debe aceptarse")
	}
}

func TestProblem_IncludesRequestID(t *testing.T) {
	r := opsServer(fakePinger{})

	rec := get(r, "/nada", map[string]string{"X-Request-Id": "demo-456"})

	if !strings.Contains(rec.Body.String(), `"request_id":"demo-456"`) {
		t.Errorf("el error debe llevar el request_id: %s", rec.Body)
	}
}
