package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDependencyFailureIsNotReadinessAndDoesNotLeakDetails(t *testing.T) {
	router := New(Dependencies{
		Database:  func(context.Context) error { return errors.New("postgres://user:secret@host/db") },
		Processor: func(context.Context) error { return nil },
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatal("dependency details leaked")
	}
	if !strings.Contains(w.Body.String(), "not_implemented") {
		t.Fatal("scaffold must not imply document checking works")
	}
}
