package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"appliance-code/services/hostservice/internal/bridge"
)

type Handler struct {
	bridge bridge.Bridge
}

func NewHandler(hostBridge bridge.Bridge) http.Handler {
	handler := &Handler{bridge: hostBridge}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.healthz)
	mux.HandleFunc("GET /internal/v1/host/info", handler.info)
	mux.HandleFunc("GET /internal/v1/host/stats", handler.stats)
	mux.HandleFunc("GET /internal/v1/host/health", handler.health)
	return mux
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	if err := h.bridge.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "host-agent",
	})
}

func (h *Handler) info(w http.ResponseWriter, r *http.Request) {
	info, err := h.bridge.Info(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.bridge.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	health, err := h.bridge.Health(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status := http.StatusOK
	if !strings.EqualFold(health.Status, "ok") {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, health)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
