package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"appliance-code/services/hostagent/internal/bridge"
	"appliance-code/services/hostagent/internal/mdns"
	"appliance-code/services/hostagent/internal/wifiap"
)

type Handler struct {
	bridge bridge.Bridge
	wifi   wifiap.Controller
	mdns   mdns.Controller
}

// NewHandler returns the host-agent HTTP API with production wifi and mdns managers.
func NewHandler(hostBridge bridge.Bridge) http.Handler {
	return NewHandlerWithControllers(hostBridge, wifiap.NewManager(), mdns.NewManager())
}

// NewHandlerWithWifi keeps older call sites working (mdns uses production manager).
func NewHandlerWithWifi(hostBridge bridge.Bridge, wifi wifiap.Controller) http.Handler {
	return NewHandlerWithControllers(hostBridge, wifi, mdns.NewManager())
}

// NewHandlerWithControllers allows tests and mains to inject controllers.
func NewHandlerWithControllers(hostBridge bridge.Bridge, wifi wifiap.Controller, mdnsCtrl mdns.Controller) http.Handler {
	if wifi == nil {
		wifi = wifiap.NewManager()
	}
	if mdnsCtrl == nil {
		mdnsCtrl = mdns.NewManager()
	}
	handler := &Handler{bridge: hostBridge, wifi: wifi, mdns: mdnsCtrl}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.healthz)
	mux.HandleFunc("GET /internal/v1/host/info", handler.info)
	mux.HandleFunc("GET /internal/v1/host/stats", handler.stats)
	mux.HandleFunc("GET /internal/v1/host/health", handler.health)
	mux.HandleFunc("GET /internal/v1/host/wifi-ap", handler.wifiAPGet)
	mux.HandleFunc("PUT /internal/v1/host/wifi-ap", handler.wifiAPPut)
	mux.HandleFunc("GET /internal/v1/host/mdns", handler.mdnsGet)
	mux.HandleFunc("PUT /internal/v1/host/mdns", handler.mdnsPut)
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

func (h *Handler) wifiAPGet(w http.ResponseWriter, r *http.Request) {
	status, err := h.wifi.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) wifiAPPut(w http.ResponseWriter, r *http.Request) {
	var req wifiap.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid wifi-ap apply body")
		return
	}
	status, err := h.wifi.Apply(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) mdnsGet(w http.ResponseWriter, r *http.Request) {
	status, err := h.mdns.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) mdnsPut(w http.ResponseWriter, r *http.Request) {
	var req mdns.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid mdns apply body")
		return
	}
	status, err := h.mdns.Apply(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
