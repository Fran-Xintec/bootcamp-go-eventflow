package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"eventflow/internal/orders/application/dto"
	"eventflow/internal/orders/domain/entity"
	"eventflow/internal/orders/domain/event"
	"eventflow/internal/orders/domain/repository"
	"eventflow/pkg/apperrors"

	"go.uber.org/zap"
)

// --- Fakes a mano (más simples que una librería de mocks) ---

type fakeRepo struct {
	mu        sync.Mutex
	orders    map[string]*entity.Order
	keys      map[string]repository.StoredKey
	saveErr   error
	saves     int
	processed map[string]bool // eventos registrados por ConfirmOrder
	// hideFirstLookup simula la carrera: la primera búsqueda de la clave no la
	// encuentra (otra petición todavía no guardaba) pero Save sí choca con ella.
	hideFirstLookup bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{orders: map[string]*entity.Order{}, keys: map[string]repository.StoredKey{}, processed: map[string]bool{}}
}

func (r *fakeRepo) IsEventProcessed(_ context.Context, eventID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.processed[eventID], nil
}

func (r *fakeRepo) ConfirmOrder(_ context.Context, eventID, orderID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.processed[eventID] {
		return false, nil
	}
	o, ok := r.orders[orderID]
	if !ok {
		return false, repository.ErrOrderNotFound
	}
	r.processed[eventID] = true
	o.Status = entity.StatusConfirmed
	return true, nil
}

func (r *fakeRepo) Save(_ context.Context, o *entity.Order, idem *repository.Idempotency) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves++
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

func (r *fakeRepo) FindByIdempotencyKey(_ context.Context, key string) (*repository.StoredKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hideFirstLookup {
		r.hideFirstLookup = false
		return nil, repository.ErrIdempotencyKeyNotFound
	}
	k, ok := r.keys[key]
	if !ok {
		return nil, repository.ErrIdempotencyKeyNotFound
	}
	return &k, nil
}

func (r *fakeRepo) FindByID(_ context.Context, id string) (*entity.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[id]
	if !ok {
		return nil, repository.ErrOrderNotFound
	}
	return o, nil
}

type fakePublisher struct {
	mu     sync.Mutex
	events []event.OrderCreated
	err    error
}

func (p *fakePublisher) Publish(_ context.Context, evt event.OrderCreated) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, evt)
	return nil
}

func (p *fakePublisher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.events)
}

func validRequest() dto.CreateOrderRequest {
	return dto.CreateOrderRequest{
		CustomerID: "cliente-1",
		Items:      []dto.ItemRequest{{SKU: "A-1", Quantity: 2, UnitPriceCents: 1000}},
	}
}

// --- CreateOrder ---

func TestCreateOrder_Success(t *testing.T) {
	repo, publisher := newFakeRepo(), &fakePublisher{}
	uc := NewCreateOrderUseCase(repo, publisher, zap.NewNop())

	res, _, err := uc.Execute(context.Background(), validRequest(), nil)
	if err != nil {
		t.Fatalf("Execute() error inesperado: %v", err)
	}

	if res.Status != "created" || res.TotalCents != 2000 || res.ID == "" {
		t.Errorf("respuesta inesperada: %+v", res)
	}
	if _, ok := repo.orders[res.ID]; !ok {
		t.Error("el pedido no quedó guardado en el repositorio")
	}
	if publisher.count() != 1 {
		t.Errorf("eventos publicados = %d, se esperaba 1", publisher.count())
	}
}

func TestCreateOrder_InvalidDoesNotSaveNorNotify(t *testing.T) {
	repo, publisher := newFakeRepo(), &fakePublisher{}
	uc := NewCreateOrderUseCase(repo, publisher, zap.NewNop())

	req := validRequest()
	req.Items = nil

	if _, _, err := uc.Execute(context.Background(), req, nil); err == nil {
		t.Fatal("se esperaba un error de validación")
	}

	if repo.saves != 0 {
		t.Errorf("Save se llamó %d veces, se esperaba 0", repo.saves)
	}
	if publisher.count() != 0 {
		t.Error("no debería publicar un evento de un pedido inválido")
	}
}

func TestCreateOrder_RepoErrorIsReturnedAndNoNotification(t *testing.T) {
	boom := errors.New("conexión caída")
	repo, publisher := newFakeRepo(), &fakePublisher{}
	repo.saveErr = boom
	uc := NewCreateOrderUseCase(repo, publisher, zap.NewNop())

	_, _, err := uc.Execute(context.Background(), validRequest(), nil)

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, se esperaba el error del repositorio", err)
	}
	if publisher.count() != 0 {
		t.Error("no debería publicar un evento si el pedido no se guardó")
	}
}

// Q-03: si el broker falla después de guardar, el pedido queda y la respuesta es exitosa.
func TestCreateOrder_PublishFailureDoesNotAffectOrder(t *testing.T) {
	repo := newFakeRepo()
	publisher := &fakePublisher{err: errors.New("broker caído")}
	uc := NewCreateOrderUseCase(repo, publisher, zap.NewNop())

	res, _, err := uc.Execute(context.Background(), validRequest(), nil)

	if err != nil {
		t.Fatalf("un fallo al publicar no debe devolver error: %v", err)
	}
	if _, ok := repo.orders[res.ID]; !ok {
		t.Error("el pedido debería seguir guardado")
	}
}

// --- Idempotencia ---

func keyPtr(k string) *string { return &k }

func TestCreateOrder_WithoutKeyCreatesEachTime(t *testing.T) {
	repo, publisher := newFakeRepo(), &fakePublisher{}
	uc := NewCreateOrderUseCase(repo, publisher, zap.NewNop())

	a, _, _ := uc.Execute(context.Background(), validRequest(), nil)
	b, _, _ := uc.Execute(context.Background(), validRequest(), nil)

	if a.ID == b.ID || len(repo.orders) != 2 {
		t.Errorf("sin clave se esperaban dos pedidos distintos, hay %d", len(repo.orders))
	}
}

func TestCreateOrder_SameKeySameBodyReplays(t *testing.T) {
	repo, publisher := newFakeRepo(), &fakePublisher{}
	uc := NewCreateOrderUseCase(repo, publisher, zap.NewNop())

	first, replayed1, err1 := uc.Execute(context.Background(), validRequest(), keyPtr("clave-1"))
	second, replayed2, err2 := uc.Execute(context.Background(), validRequest(), keyPtr("clave-1"))

	if err1 != nil || err2 != nil {
		t.Fatalf("errores inesperados: %v, %v", err1, err2)
	}
	if replayed1 || !replayed2 {
		t.Errorf("replayed = %v y %v, se esperaba false y true", replayed1, replayed2)
	}
	if first.ID != second.ID || *first != *second {
		t.Errorf("el reintento debe devolver el mismo pedido: %+v y %+v", first, second)
	}
	if len(repo.orders) != 1 {
		t.Errorf("pedidos guardados = %d, se esperaba 1", len(repo.orders))
	}
	if publisher.count() != 1 {
		t.Errorf("notificaciones = %d, el reintento no debe publicar otro evento", publisher.count())
	}
}

func TestCreateOrder_SameKeyDifferentBodyIsRejected(t *testing.T) {
	repo := newFakeRepo()
	uc := NewCreateOrderUseCase(repo, &fakePublisher{}, zap.NewNop())

	_, _, _ = uc.Execute(context.Background(), validRequest(), keyPtr("clave-1"))

	other := validRequest()
	other.Items[0].Quantity = 9
	_, _, err := uc.Execute(context.Background(), other, keyPtr("clave-1"))

	var domainErr *apperrors.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != "IDEMPOTENCY_KEY_REUSED" || domainErr.Type != apperrors.ErrorTypeValidation {
		t.Fatalf("err = %v, se esperaba IDEMPOTENCY_KEY_REUSED", err)
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
			repo := newFakeRepo()
			uc := NewCreateOrderUseCase(repo, &fakePublisher{}, zap.NewNop())

			_, _, err := uc.Execute(context.Background(), validRequest(), keyPtr(tc.key))

			var domainErr *apperrors.DomainError
			if !errors.As(err, &domainErr) || domainErr.Code != "IDEMPOTENCY_KEY_INVALID" || domainErr.Type != apperrors.ErrorTypeBadRequest {
				t.Fatalf("err = %v, se esperaba IDEMPOTENCY_KEY_INVALID", err)
			}
			if len(repo.orders) != 0 {
				t.Error("no debería haberse creado ningún pedido")
			}
		})
	}
}

// Dos peticiones simultáneas: la segunda no encuentra la clave al buscar, pero
// choca al guardar. Debe responder como un reintento, sin crear otro pedido.
func TestCreateOrder_KeyTakenWhileSavingReplays(t *testing.T) {
	repo, publisher := newFakeRepo(), &fakePublisher{}
	uc := NewCreateOrderUseCase(repo, publisher, zap.NewNop())

	first, _, _ := uc.Execute(context.Background(), validRequest(), keyPtr("clave-1"))

	repo.hideFirstLookup = true
	second, replayed, err := uc.Execute(context.Background(), validRequest(), keyPtr("clave-1"))

	if err != nil || !replayed || second.ID != first.ID {
		t.Fatalf("se esperaba un reintento con el mismo pedido: %+v, replayed=%v, err=%v", second, replayed, err)
	}
	if len(repo.orders) != 1 || publisher.count() != 1 {
		t.Errorf("pedidos = %d, eventos = %d; se esperaba 1 y 1", len(repo.orders), publisher.count())
	}
}

// --- GetOrder ---

func TestGetOrder_Found(t *testing.T) {
	repo := newFakeRepo()
	order, _ := entity.NewOrder("c1", []entity.Item{{SKU: "A", Quantity: 1, UnitPriceCents: 500}})
	repo.orders[order.ID] = order

	res, err := NewGetOrderUseCase(repo).Execute(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("Execute() error inesperado: %v", err)
	}
	if res.ID != order.ID || res.TotalCents != 500 || len(res.Items) != 1 {
		t.Errorf("respuesta inesperada: %+v", res)
	}
}

func TestGetOrder_Errors(t *testing.T) {
	uc := NewGetOrderUseCase(newFakeRepo())

	tests := []struct {
		name     string
		id       string
		wantType apperrors.ErrorType
	}{
		{"no existe", "6f1b0f7e-8f5c-4c37-9c63-1f0d9d0a1111", apperrors.ErrorTypeNotFound},
		{"id mal formado", "no-soy-un-uuid", apperrors.ErrorTypeBadRequest},
		{"id vacío", "", apperrors.ErrorTypeBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.Execute(context.Background(), tc.id)

			var domainErr *apperrors.DomainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("se esperaba un DomainError, llegó: %v", err)
			}
			if domainErr.Type != tc.wantType {
				t.Errorf("Type = %q, se esperaba %q", domainErr.Type, tc.wantType)
			}
		})
	}
}
