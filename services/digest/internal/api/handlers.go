// Package api implements the HTTP handlers for the digest service.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/rutabageldev/navi/services/digest/internal/api/gen"
	natnats "github.com/rutabageldev/navi/services/internal/nats"
	"github.com/rutabageldev/navi/services/internal/postgres"
	"github.com/rutabageldev/navi/services/internal/vault"

	"github.com/jackc/pgx/v5/pgxpool"
	gonanats "github.com/nats-io/nats.go"
)

// Handler implements gen.ServerInterface for the digest service health endpoints.
type Handler struct {
	version string
	vault   *vault.Client
	db      *pgxpool.Pool
	nats    *gonanats.Conn
}

// NewHandler creates a Handler with the given dependencies.
func NewHandler(v *vault.Client, db *pgxpool.Pool, nc *gonanats.Conn, version string) *Handler {
	return &Handler{version: version, vault: v, db: db, nats: nc}
}

// HealthLive handles GET /v1/health/live.
// Always returns 200 if the process is running.
func (h *Handler) HealthLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, gen.LiveResponse{Status: gen.LiveResponseStatusOk})
}

// HealthReady handles GET /v1/health/ready.
// Runs dependency health checks in parallel and returns 200 if all pass, 503 if any fail.
func (h *Handler) HealthReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type result struct {
		name string
		err  error
	}

	checks := []struct {
		name string
		fn   func() error
	}{
		{"postgres", func() error { return postgres.HealthCheck(ctx, h.db) }},
		{"nats", func() error { return natnats.HealthCheck(h.nats) }},
		{"vault", func() error { return h.vault.Ping(ctx) }},
	}

	results := make([]result, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func(i int, name string, fn func() error) {
			defer wg.Done()
			results[i] = result{name: name, err: fn()}
		}(i, c.name, c.fn)
	}
	wg.Wait()

	ok := true
	resp := gen.ReadyResponse{
		Version: h.version,
		Status:  gen.ReadyResponseStatusOk,
	}

	for _, res := range results {
		msg := "ok"
		if res.err != nil {
			ok = false
			msg = res.err.Error()
			slog.Error("dependency health check failed",
				"check", res.name,
				"error", res.err,
			)
		}
		switch res.name {
		case "postgres":
			resp.Checks.Postgres = &msg
		case "nats":
			resp.Checks.Nats = &msg
		case "vault":
			resp.Checks.Vault = &msg
		}
	}

	status := http.StatusOK
	if !ok {
		resp.Status = gen.ReadyResponseStatusDegraded
		status = http.StatusServiceUnavailable
	}

	writeJSON(w, status, resp)
}

// writeJSON writes v as JSON to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}
