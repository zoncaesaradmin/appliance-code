package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"appliance-code/services/hostservice/internal/host"
)

type Handler struct {
	hostRoot string
}

func NewHandler(hostRoot string) http.Handler {
	handler := &Handler{hostRoot: hostRoot}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.healthz)
	mux.HandleFunc("GET /internal/v1/host/info", handler.info)
	mux.HandleFunc("GET /internal/v1/host/stats", handler.stats)
	mux.HandleFunc("GET /internal/v1/host/health", handler.health)
	return mux
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "appliance-host-service",
	})
}

func (h *Handler) info(w http.ResponseWriter, _ *http.Request) {
	info, err := host.CollectInfo(h.hostRoot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) stats(w http.ResponseWriter, _ *http.Request) {
	stats, err := host.CollectStats(h.hostRoot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	health := host.CollectHealth(h.hostRoot)
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
