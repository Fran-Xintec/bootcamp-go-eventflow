package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"eventflow/internal/orders/application/usecase"
	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/event"
	"eventflow/internal/orders/domain/repository"
	"eventflow/internal/orders/presentation/handler"
	"eventflow/internal/orders/presentation/router"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Repositorio y notificador fakes: aquí solo interesa el contrato HTTP.
type fakeRepo struct {
	mu      sync.Mutex
	orders  map[string]*entity.Order
	keys    map[string]repository.StoredKey
	saveErr error
	findErr error
	slow    bool // si es true, Save espera a que se cancele el contexto
}

func (r *fakeRepo) Save(ctx context.Context, o *entity.Order, idem *repository.Idempotency) error {
	if r.slow {
		<-ctx.Done()
		return ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	if idem != nil {
		if _, taken := r.keys[idem.Key]; taken {
			return repository.ErrIdempotencyKeyTaken
		}
		r.keys[idem.Key] = repository.StoredKey{RequestHash: idem.RequestHash, OrderID: o.ID}
	}
	r.orders[o.ID] = o
	return nil
}

func (r *fakeRepo) IsEventProcessed(context.Context, string) (bool, error) { return false, nil }

func (r *fakeRepo) ConfirmOrder(context.Context, string, string) (bool, error) { return true, nil }

func (r *fakeRepo) FindByIdempotencyKey(_ context.Context, key string) (*repository.StoredKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k, ok := r.keys[key]
	if !ok {
		return nil, repository.ErrIdempotencyKeyNotFound
	}
	return &k, nil
}

func (r *fakeRepo) FindByID(_ context.Context, id string) (*entity.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	o, ok := r.orders[id]
	if !ok {
		return nil, repository.ErrOrderNotFound
	}
	return o, nil
}

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, event.OrderCreated) error { return nil }

// newTestServer arma el router real de Gin con casos de uso reales y el repositorio fake.
func newTestServer(repo *fakeRepo) (*gin.Engine, *usecase.CreateOrderUseCase) {
	return newTestServerWithTimeout(repo, 5*time.Second)
}

func newTestServerWithTimeout(repo *fakeRepo, timeout time.Duration) (*gin.Engine, *usecase.CreateOrderUseCase) {
	gin.SetMode(gin.TestMode)
	log := zap.NewNop()

	create := usecase.NewCreateOrderUseCase(repo, noopPublisher{}, log)
	get := usecase.NewGetOrderUseCase(repo)

	r := gin.New()
	router.RegisterRoutes(r, handler.NewOrderHandler(create, get, log, timeout))
	return r, create
}

func newRepo() *fakeRepo {
	return &fakeRepo{orders: map[string]*entity.Order{}, keys: map[string]repository.StoredKey{}}
}

func do(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	return doWithHeaders(r, method, path, body, nil)
}

func doWithHeaders(r http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header[k] = []string{v} // asignación directa: permite probar una cabecera vacía
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

const validBody = `{"customer_id":"cliente-1","items":[{"sku":"A-1","quantity":2,"unit_price_cents":1500},{"sku":"B-2","quantity":1,"unit_price_cents":999}]}`

func TestCreateOrder_Created(t *testing.T) {
	repo := newRepo()
	r, _ := newTestServer(repo)

	rec := do(r, http.MethodPost, "/orders", validBody)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, se esperaba 201. Cuerpo: %s", rec.Code, rec.Body)
	}

	var res struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		TotalCents int64  `json:"total_cents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("la respuesta no es JSON: %v", err)
	}
	if res.ID == "" || res.Status != "created" || res.TotalCents != 3999 {
		t.Errorf("respuesta inesperada: %+v", res)
	}
	if len(repo.orders) != 1 {
		t.Errorf("pedidos guardados = %d, se esperaba 1", len(repo.orders))
	}
	if got := rec.Header().Get("Location"); got != "/orders/"+res.ID {
		t.Errorf("Location = %q, se esperaba /orders/%s", got, res.ID)
	}
}

func TestCreateOrder_InvalidRequests(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"JSON roto", `{"customer_id": `, http.StatusBadRequest, "INVALID_JSON"},
		{"cuerpo vacío", ``, http.StatusBadRequest, "INVALID_JSON"},
		{"tipo equivocado", `{"customer_id":"c1","items":[{"sku":"A","quantity":"dos","unit_price_cents":100}]}`, http.StatusBadRequest, "INVALID_JSON"},
		{"cliente null", `{"customer_id":null,"items":[{"sku":"A","quantity":1,"unit_price_cents":100}]}`, http.StatusUnprocessableEntity, "ORDER_CUSTOMER_REQUIRED"},
		{"lista null", `{"customer_id":"c1","items":null}`, http.StatusUnprocessableEntity, "ORDER_ITEMS_REQUIRED"},
		{"sin cliente", `{"items":[{"sku":"A","quantity":1,"unit_price_cents":100}]}`, http.StatusUnprocessableEntity, "ORDER_CUSTOMER_REQUIRED"},
		{"lista vacía", `{"customer_id":"c1","items":[]}`, http.StatusUnprocessableEntity, "ORDER_ITEMS_REQUIRED"},
		{"sin lista", `{"customer_id":"c1"}`, http.StatusUnprocessableEntity, "ORDER_ITEMS_REQUIRED"},
		{"cantidad cero", `{"customer_id":"c1","items":[{"sku":"A","quantity":0,"unit_price_cents":100}]}`, http.StatusUnprocessableEntity, "ORDER_ITEM_INVALID"},
		{"cantidad negativa", `{"customer_id":"c1","items":[{"sku":"A","quantity":-1,"unit_price_cents":100}]}`, http.StatusUnprocessableEntity, "ORDER_ITEM_INVALID"},
		{"precio cero", `{"customer_id":"c1","items":[{"sku":"A","quantity":1,"unit_price_cents":0}]}`, http.StatusUnprocessableEntity, "ORDER_ITEM_INVALID"},
		{"precio negativo", `{"customer_id":"c1","items":[{"sku":"A","quantity":1,"unit_price_cents":-5}]}`, http.StatusUnprocessableEntity, "ORDER_ITEM_INVALID"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo()
			r, _ := newTestServer(repo)

			rec := do(r, http.MethodPost, "/orders", tc.body)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, se esperaba %d. Cuerpo: %s", rec.Code, tc.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, se esperaba application/problem+json", ct)
			}
			var res struct {
				Code   string `json:"code"`
				Detail string `json:"detail"`
				Status int    `json:"status"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatalf("el error no tiene el formato común: %v", err)
			}
			if res.Code != tc.wantCode || res.Detail == "" || res.Status != tc.wantStatus {
				t.Errorf("error = %+v, se esperaba code %q y status %d", res, tc.wantCode, tc.wantStatus)
			}
			if len(repo.orders) != 0 {
				t.Error("no debería haberse creado ningún pedido")
			}
		})
	}
}

func TestCreateOrder_RequiresJSONContentType(t *testing.T) {
	repo := newRepo()
	r, _ := newTestServer(repo)

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(validBody))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, se esperaba 415", rec.Code)
	}
	if len(repo.orders) != 0 {
		t.Error("no debería haberse creado ningún pedido")
	}
}

// El error interno de la base nunca debe llegar al cliente (RF-05).
func TestCreateOrder_InternalErrorDoesNotLeakDetails(t *testing.T) {
	repo := newRepo()
	repo.saveErr = errors.New(`pq: password authentication failed for user "admin" (SQL: INSERT INTO orders)`)
	r, _ := newTestServer(repo)

	rec := do(r, http.MethodPost, "/orders", validBody)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, se esperaba 500", rec.Code)
	}
	body := rec.Body.String()
	for _, secret := range []string{"password", "admin", "INSERT", "SQL"} {
		if strings.Contains(body, secret) {
			t.Errorf("la respuesta expone %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, "INTERNAL_001") {
		t.Errorf("se esperaba el código genérico INTERNAL_001: %s", body)
	}
}

func TestGetOrder(t *testing.T) {
	repo := newRepo()
	r, _ := newTestServer(repo)

	created := do(r, http.MethodPost, "/orders", validBody)
	var c struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &c)

	t.Run("existente", func(t *testing.T) {
		rec := do(r, http.MethodGet, "/orders/"+c.ID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, se esperaba 200. Cuerpo: %s", rec.Code, rec.Body)
		}
		var res struct {
			ID         string `json:"id"`
			CustomerID string `json:"customer_id"`
			Status     string `json:"status"`
			TotalCents int64  `json:"total_cents"`
			Items      []struct {
				SKU string `json:"sku"`
			} `json:"items"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if res.ID != c.ID || res.CustomerID != "cliente-1" || res.Status != "created" || res.TotalCents != 3999 || len(res.Items) != 2 {
			t.Errorf("respuesta inesperada: %+v", res)
		}
	})

	t.Run("no existe", func(t *testing.T) {
		rec := do(r, http.MethodGet, "/orders/6f1b0f7e-8f5c-4c37-9c63-1f0d9d0a1111", "")
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, se esperaba 404", rec.Code)
		}
	})

	t.Run("id mal formado", func(t *testing.T) {
		rec := do(r, http.MethodGet, "/orders/abc", "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, se esperaba 400", rec.Code)
		}
	})
}

func TestGetOrder_InternalErrorDoesNotLeakDetails(t *testing.T) {
	repo := newRepo()
	repo.findErr = errors.New("dial tcp 10.0.0.5:5432: connection refused")
	r, _ := newTestServer(repo)

	rec := do(r, http.MethodGet, "/orders/6f1b0f7e-8f5c-4c37-9c63-1f0d9d0a1111", "")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, se esperaba 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("la respuesta expone la dirección interna: %s", rec.Body)
	}
}

// --- Idempotencia, límites y plazos ---

func orderID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("la respuesta no es JSON: %v", err)
	}
	return res.ID
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var res struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("el error no tiene el formato común: %v", err)
	}
	return res.Code
}

func TestCreateOrder_IdempotentRetry(t *testing.T) {
	repo := newRepo()
	r, _ := newTestServer(repo)
	headers := map[string]string{"Idempotency-Key": "clave-1"}

	first := doWithHeaders(r, http.MethodPost, "/orders", validBody, headers)
	second := doWithHeaders(r, http.MethodPost, "/orders", validBody, headers)

	if first.Code != http.StatusCreated || second.Code != http.StatusOK {
		t.Fatalf("status = %d y %d, se esperaba 201 y 200", first.Code, second.Code)
	}
	if first.Header().Get("Idempotent-Replayed") != "" {
		t.Error("la primera respuesta no debe marcarse como reintento")
	}
	if second.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("el reintento debe traer Idempotent-Replayed: true")
	}
	id := orderID(t, first)
	if orderID(t, second) != id {
		t.Error("el reintento debe devolver el mismo pedido")
	}
	if len(repo.orders) != 1 {
		t.Errorf("pedidos guardados = %d, se esperaba 1", len(repo.orders))
	}
}

func TestCreateOrder_WithoutKeyCreatesTwoOrders(t *testing.T) {
	repo := newRepo()
	r, _ := newTestServer(repo)

	do(r, http.MethodPost, "/orders", validBody)
	do(r, http.MethodPost, "/orders", validBody)

	if len(repo.orders) != 2 {
		t.Errorf("pedidos guardados = %d, se esperaban 2", len(repo.orders))
	}
}

func TestCreateOrder_KeyReusedWithDifferentBody(t *testing.T) {
	repo := newRepo()
	r, _ := newTestServer(repo)
	headers := map[string]string{"Idempotency-Key": "clave-1"}

	doWithHeaders(r, http.MethodPost, "/orders", validBody, headers)
	other := `{"customer_id":"cliente-2","items":[{"sku":"Z","quantity":1,"unit_price_cents":5}]}`
	rec := doWithHeaders(r, http.MethodPost, "/orders", other, headers)

	if rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("status = %d, cuerpo = %s; se esperaba 422 IDEMPOTENCY_KEY_REUSED", rec.Code, rec.Body)
	}
	if len(repo.orders) != 1 {
		t.Errorf("pedidos guardados = %d, se esperaba 1", len(repo.orders))
	}
}

func TestCreateOrder_InvalidKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"vacía", ""},
		{"de 129 caracteres", strings.Repeat("k", 129)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepo()
			r, _ := newTestServer(repo)

			rec := doWithHeaders(r, http.MethodPost, "/orders", validBody, map[string]string{"Idempotency-Key": tc.key})

			if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "IDEMPOTENCY_KEY_INVALID" {
				t.Fatalf("status = %d, cuerpo = %s; se esperaba 400 IDEMPOTENCY_KEY_INVALID", rec.Code, rec.Body)
			}
			if len(repo.orders) != 0 {
				t.Error("no debería haberse creado ningún pedido")
			}
		})
	}
}

func TestCreateOrder_BodyTooLarge(t *testing.T) {
	repo := newRepo()
	r, _ := newTestServer(repo)

	big := `{"customer_id":"c1","items":[{"sku":"` + strings.Repeat("x", 1<<20) + `","quantity":1,"unit_price_cents":1}]}`
	rec := do(r, http.MethodPost, "/orders", big)

	if rec.Code != http.StatusRequestEntityTooLarge || errorCode(t, rec) != "REQUEST_TOO_LARGE" {
		t.Fatalf("status = %d, cuerpo = %.120s; se esperaba 413 REQUEST_TOO_LARGE", rec.Code, rec.Body)
	}
	if len(repo.orders) != 0 {
		t.Error("no debería haberse creado ningún pedido")
	}
}

func TestCreateOrder_RequestTimeout(t *testing.T) {
	repo := newRepo()
	repo.slow = true
	r, _ := newTestServerWithTimeout(repo, 50*time.Millisecond)

	rec := do(r, http.MethodPost, "/orders", validBody)

	if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "REQUEST_TIMEOUT" {
		t.Fatalf("status = %d, cuerpo = %s; se esperaba 503 REQUEST_TIMEOUT", rec.Code, rec.Body)
	}
}
