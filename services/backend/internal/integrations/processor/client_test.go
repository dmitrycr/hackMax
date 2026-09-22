package processor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestReadyDoesNotForwardServiceTokenOnRedirect(t *testing.T) {
	var contacted atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted.Store(true) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	if err := New(source.URL, "private-token").Ready(context.Background()); err == nil {
		t.Fatal("redirect must fail")
	}
	if contacted.Load() {
		t.Fatal("service credentials must not be forwarded to a redirect target")
	}
}
