package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Checker func(context.Context) error

type Dependencies struct {
	Database  Checker
	Processor Checker
}

type Component struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type Status struct {
	Service    string      `json:"service"`
	Stage      string      `json:"stage"`
	Components []Component `json:"components"`
}

func New(deps Dependencies) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]string{"status": "ok", "service": "go-api"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		status, ready := collect(r.Context(), deps)
		code := http.StatusOK
		if !ready {
			code = http.StatusServiceUnavailable
		}
		respond(w, code, status)
	})
	r.Get("/api/v1/system", func(w http.ResponseWriter, r *http.Request) {
		status, _ := collect(r.Context(), deps)
		respond(w, http.StatusOK, status)
	})
	return r
}

func collect(parent context.Context, deps Dependencies) (Status, bool) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	status := Status{Service: "hackmax", Stage: "scaffold", Components: []Component{{Name: "go-api", Status: "ready"}}}
	ready := true
	for _, item := range []struct {
		name  string
		check Checker
	}{{"postgres", deps.Database}, {"document-processor", deps.Processor}} {
		state := "ready"
		if item.check == nil || item.check(ctx) != nil {
			state = "unavailable"
			ready = false
		}
		status.Components = append(status.Components, Component{Name: item.name, Status: state})
	}
	status.Components = append(status.Components, Component{Name: "document-checking", Status: "not_implemented"}, Component{Name: "max-integration", Status: "not_implemented"})
	return status, ready
}

func respond(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}
