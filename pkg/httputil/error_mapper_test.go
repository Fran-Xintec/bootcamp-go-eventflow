package httputil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"eventflow/pkg/apperrors"
)

func TestMapErrorToHttp(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"petición ilegible", apperrors.NewBadRequestError("X", "m"), http.StatusBadRequest, "X"},
		{"regla de contenido", apperrors.NewValidationError("X", "m"), http.StatusUnprocessableEntity, "X"},
		{"no existe", apperrors.NewNotFoundError("X", "m"), http.StatusNotFound, "X"},
		{"conflicto", apperrors.NewConflictError("X", "m"), http.StatusConflict, "X"},
		{"media type", apperrors.NewUnsupportedMediaTypeError("X", "m"), http.StatusUnsupportedMediaType, "X"},
		{"cuerpo demasiado grande", apperrors.NewPayloadTooLargeError("X", "m"), http.StatusRequestEntityTooLarge, "X"},
		{"servicio no disponible", apperrors.NewUnavailableError("X", "m"), http.StatusServiceUnavailable, "X"},
		{"plazo agotado", fmt.Errorf("consultando: %w", context.DeadlineExceeded), http.StatusServiceUnavailable, "REQUEST_TIMEOUT"},
		{"envuelto con %w", fmt.Errorf("contexto: %w", apperrors.NewNotFoundError("X", "m")), http.StatusNotFound, "X"},
		{"error desconocido", errors.New("detalle interno con SQL"), http.StatusInternalServerError, "INTERNAL_001"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := MapErrorToHttp(tc.err)
			if p.Status != tc.wantStatus || p.Code != tc.wantCode {
				t.Errorf("status = %d, code = %q; se esperaba %d y %q", p.Status, p.Code, tc.wantStatus, tc.wantCode)
			}
			if p.Title != http.StatusText(tc.wantStatus) || p.Type != "/problems/"+tc.wantCode {
				t.Errorf("title = %q, type = %q", p.Title, p.Type)
			}
			if tc.wantStatus == http.StatusInternalServerError && p.Detail == "detalle interno con SQL" {
				t.Error("el 500 no debe exponer el detalle del error")
			}
		})
	}
}
