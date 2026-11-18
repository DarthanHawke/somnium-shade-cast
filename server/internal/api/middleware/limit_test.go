package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
)

// requestState хранит состояние тестового запроса и позволяет вручную завершить его
// обработку.
type requestState struct {
	entered  bool
	finished bool
	release  chan struct{}
	response *httptest.ResponseRecorder
}

// harness создаёт один обработчик с Limit и возвращает функцию запуска запросов.
// Каждый допущенный запрос удерживает слот до закрытия своего канала release.
func harness(concurrent, waiting int) func(context.Context) *requestState {
	key := new(int)
	h := Limit(concurrent, waiting)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state := r.Context().Value(key).(*requestState)
		state.entered = true
		<-state.release
		w.WriteHeader(http.StatusNoContent)
	}))
	return func(ctx context.Context) *requestState {
		state := &requestState{release: make(chan struct{}), response: httptest.NewRecorder()}
		r := httptest.NewRequest("POST", "/", nil).WithContext(context.WithValue(ctx, key, state))
		go func() {
			h.ServeHTTP(state.response, r)
			state.finished = true
		}()
		synctest.Wait()
		return state
	}
}

// rejected проверяет, что запрос завершился до входа в обработчик с ответом 503 и
// Retry-After: 5.
func rejected(t *testing.T, s *requestState) {
	t.Helper()
	if s.entered || !s.finished || s.response.Code != http.StatusServiceUnavailable || s.response.Header().Get("Retry-After") != "5" {
		t.Fatalf("expected rejection, got %+v", s)
	}
}

// TestZeroQueueAllowsConcurrentRequests проверяет, что при отключённой очереди два запроса
// занимают свободные слоты, а третий отклоняется.
func TestZeroQueueAllowsConcurrentRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := harness(2, 0)
		a, b := start(context.Background()), start(context.Background())
		defer close(a.release)
		defer close(b.release)
		if !a.entered || !b.entered {
			t.Fatal("free execution slots rejected")
		}
		rejected(t, start(context.Background()))
	})
}

// TestQueueCapacityAndReleaseOnExecution проверяет, что ожидающий запрос освобождает место
// в очереди сразу после перехода к выполнению.
func TestQueueCapacityAndReleaseOnExecution(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := harness(1, 1)
		a := start(context.Background())
		b := start(context.Background())
		defer close(b.release)
		if !a.entered || b.entered || b.finished {
			t.Fatal("incorrect initial queue state")
		}
		rejected(t, start(context.Background()))
		close(a.release)
		synctest.Wait()
		if !b.entered {
			t.Fatal("queued request did not start")
		}
		c := start(context.Background())
		defer close(c.release)
		if c.entered || c.finished {
			t.Fatal("queue slot not released on execution")
		}
		rejected(t, start(context.Background()))
	})
}

// TestCancelledWaiterReleasesQueue проверяет, что отменённый запрос освобождает место в
// очереди до входа в обработчик.
func TestCancelledWaiterReleasesQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := harness(1, 1)
		a := start(context.Background())
		ctx, cancel := context.WithCancel(context.Background())
		b := start(ctx)
		cancel()
		synctest.Wait()
		if !b.finished || b.entered {
			t.Fatal("cancelled waiter reached handler or did not exit")
		}
		c := start(context.Background())
		defer close(c.release)
		if c.finished || c.entered {
			t.Fatal("cancelled waiter leaked queue slot")
		}
		close(a.release)
		synctest.Wait()
		if !c.entered {
			t.Fatal("replacement waiter did not start")
		}
	})
}

// TestAlreadyCancelledRequestDoesNotRun проверяет, что запрос с уже отменённым контекстом
// не доходит до обработчика.
func TestAlreadyCancelledRequestDoesNotRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	h := Limit(1, 0)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil).WithContext(ctx))
	if called {
		t.Fatal("cancelled request executed")
	}
}

// TestPanicReleasesExecutionSlot проверяет, что паника обработчика распространяется наружу,
// а следующий запрос может занять освобождённый слот.
func TestPanicReleasesExecutionSlot(t *testing.T) {
	calls := 0
	h := Limit(1, 0)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			panic("test")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected handler panic")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil))
	}()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	if calls != 2 || w.Code != http.StatusNoContent {
		t.Fatal("panic leaked execution slot")
	}
}
