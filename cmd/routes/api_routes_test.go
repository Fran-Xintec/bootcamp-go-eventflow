package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFallbacks_UseCommonErrorFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/orders", func(c *gin.Context) { c.Status(http.StatusOK) })
	registerFallbacks(r)

	tests := []struct {
		name      string
		method    string
		path      string
		wantCode  int
		wantErr   string
		wantAllow string
	}{
		{"ruta inexistente", http.MethodGet, "/nada", http.StatusNotFound, "ROUTE_NOT_FOUND", ""},
		{"método no permitido", http.MethodDelete, "/orders", http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, tc.wantCode)
			}
			if tc.wantAllow != "" && rec.Header().Get("Allow") != tc.wantAllow {
				t.Errorf("Allow = %q, se esperaba %q", rec.Header().Get("Allow"), tc.wantAllow)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, se esperaba application/problem+json", ct)
			}
			var res struct {
				Code   string `json:"code"`
				Status int    `json:"status"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || res.Code != tc.wantErr || res.Status != tc.wantCode {
				t.Errorf("cuerpo = %s, se esperaba code %q", rec.Body, tc.wantErr)
			}
		})
	}
}
