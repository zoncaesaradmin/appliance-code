package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// ReadinessChecker reports whether the server can currently accept its
// owned traffic (e.g. storage is reachable and writable).
type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

// StartupState tracks whether the process has finished migrations and
// initialization, so the startup probe can distinguish "still starting"
// from "started but temporarily not ready."
type StartupState struct {
	done atomic.Bool
}

// MarkStarted records that initialization has completed.
func (s *StartupState) MarkStarted() { s.done.Store(true) }

// Started reports whether MarkStarted has been called.
func (s *StartupState) Started() bool { return s.done.Load() }

// RegisterHealthRoutes registers liveness, readiness, and startup probes on
// mux. These must only be reachable on the internal listener, never through
// public ingress, per the plan's health-endpoint requirement.
func RegisterHealthRoutes(mux *http.ServeMux, checker ReadinessChecker, startup *StartupState) {
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		// Liveness proves only that the process event loop is healthy; it
		// must not fail because a dependency is unavailable.
		writeHealthStatus(w, http.StatusOK, "ok")
	})

	mux.HandleFunc("GET /health/startup", func(w http.ResponseWriter, r *http.Request) {
		if !startup.Started() {
			writeHealthStatus(w, http.StatusServiceUnavailable, "starting")
			return
		}
		writeHealthStatus(w, http.StatusOK, "started")
	})

	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if !startup.Started() {
			writeHealthStatus(w, http.StatusServiceUnavailable, "starting")
			return
		}
		if err := checker.Ready(r.Context()); err != nil {
			writeHealthStatus(w, http.StatusServiceUnavailable, "not ready")
			return
		}
		writeHealthStatus(w, http.StatusOK, "ready")
	})
}

func writeHealthStatus(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
	}{Status: message})
}
