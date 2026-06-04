package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChainExecutesInOuterToInnerOrder(t *testing.T) {
	var order []string
	mk := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, "in:"+name)
				next.ServeHTTP(w, r)
				order = append(order, "out:"+name)
			})
		}
	}
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(204)
	})
	h := Chain(mk("a"), mk("b"))(final)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	want := []string{"in:a", "in:b", "handler", "out:b", "out:a"}
	if len(order) != len(want) {
		t.Fatalf("order=%v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d]=%q want %q", i, order[i], want[i])
		}
	}
}

func TestChainNoMiddlewaresReturnsHandler(t *testing.T) {
	called := false
	h := Chain()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Fatal("inner handler not called")
	}
}
