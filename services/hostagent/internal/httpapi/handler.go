package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"appliance-code/services/hostagent/internal/bridge"
	"appliance-code/services/hostagent/internal/firewall"
	"appliance-code/services/hostagent/internal/internalauth"
	"appliance-code/services/hostagent/internal/mdns"
	"appliance-code/services/hostagent/internal/wifiap"
	"appliance-code/services/hostagent/internal/wificlient"
)

type Handler struct {
	bridge        bridge.Bridge
	wifiClient    wificlient.Controller
	wifiAP        wifiap.Controller
	mdns          mdns.Controller
	internalToken string
	trustedSocket bool
}

// NewHandler returns the host-agent HTTP API with production wifi and mdns managers.
func NewHandler(hostBridge bridge.Bridge) http.Handler {
	return NewHandlerWithInternalToken(hostBridge, wificlient.NewManager(), wifiap.NewManager(), mdns.NewManager(), "")
}

// NewHandlerWithWifi keeps older call sites working (mdns uses production manager).
func NewHandlerWithWifi(hostBridge bridge.Bridge, wifi wifiap.Controller) http.Handler {
	return NewHandlerWithInternalToken(hostBridge, wificlient.NewManager(), wifi, mdns.NewManager(), "")
}

// NewHandlerWithControllers allows tests and mains to inject controllers.
func NewHandlerWithControllers(hostBridge bridge.Bridge, wifiClient wificlient.Controller, wifiAP wifiap.Controller, mdnsCtrl mdns.Controller) http.Handler {
	return NewHandlerWithInternalToken(hostBridge, wifiClient, wifiAP, mdnsCtrl, "")
}

// NewHandlerWithInternalToken protects the privileged firewall projection with
// a control-plane-only credential. The public host-agent pod never receives an
// unqualified port-opening request.
func NewHandlerWithInternalToken(hostBridge bridge.Bridge, wifiClient wificlient.Controller, wifiAP wifiap.Controller, mdnsCtrl mdns.Controller, internalToken string) http.Handler {
	return newHandler(hostBridge, wifiClient, wifiAP, mdnsCtrl, internalToken, false)
}

// NewUnixSocketHandler enables firewall projection only for callers which can
// open the root-owned, group-restricted host-agent daemon socket. The network
// facing host-agent pod still requires the control-plane credential.
func NewUnixSocketHandler(hostBridge bridge.Bridge, wifiClient wificlient.Controller, wifiAP wifiap.Controller, mdnsCtrl mdns.Controller) http.Handler {
	return newHandler(hostBridge, wifiClient, wifiAP, mdnsCtrl, "", true)
}

func newHandler(hostBridge bridge.Bridge, wifiClient wificlient.Controller, wifiAP wifiap.Controller, mdnsCtrl mdns.Controller, internalToken string, trustedSocket bool) http.Handler {
	if wifiClient == nil {
		wifiClient = wificlient.NewManager()
	}
	if wifiAP == nil {
		wifiAP = wifiap.NewManager()
	}
	if mdnsCtrl == nil {
		mdnsCtrl = mdns.NewManager()
	}
	handler := &Handler{bridge: hostBridge, wifiClient: wifiClient, wifiAP: wifiAP, mdns: mdnsCtrl, internalToken: strings.TrimSpace(internalToken), trustedSocket: trustedSocket}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.healthz)
	mux.HandleFunc("GET /internal/v1/host/info", handler.info)
	mux.HandleFunc("GET /internal/v1/host/stats", handler.stats)
	mux.HandleFunc("GET /internal/v1/host/health", handler.health)
	mux.HandleFunc("GET /internal/v1/host/wifi", handler.wifiGet)
	mux.HandleFunc("PUT /internal/v1/host/wifi/enable", handler.wifiEnable)
	mux.HandleFunc("PUT /internal/v1/host/wifi", handler.wifiPut)
	mux.HandleFunc("GET /internal/v1/host/wifi/scan", handler.wifiScan)
	mux.HandleFunc("GET /internal/v1/host/wifi-ap", handler.wifiAPGet)
	mux.HandleFunc("PUT /internal/v1/host/wifi-ap", handler.wifiAPPut)
	mux.HandleFunc("GET /internal/v1/host/mdns", handler.mdnsGet)
	mux.HandleFunc("PUT /internal/v1/host/mdns", handler.mdnsPut)
	mux.HandleFunc("GET /internal/v1/host/application-firewall/{application}", handler.applicationFirewallGet)
	mux.HandleFunc("PUT /internal/v1/host/application-firewall/{application}", handler.applicationFirewallPut)
	mux.HandleFunc("PUT /internal/v1/host/application-mdns/{application}", handler.applicationMDNSPut)
	return mux
}

func (h *Handler) applicationMDNSPut(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeError(w, http.StatusUnauthorized, "internal authentication failed")
		return
	}
	var request mdns.ApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid application mdns body")
		return
	}
	if request.Application != r.PathValue("application") {
		writeError(w, http.StatusBadRequest, "application mdns path and body must match")
		return
	}
	if err := h.bridge.ApplicationMDNSApply(r.Context(), request); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) applicationFirewallGet(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeError(w, http.StatusUnauthorized, "internal authentication failed")
		return
	}
	status, err := h.bridge.ApplicationFirewallStatus(r.Context(), r.PathValue("application"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) applicationFirewallPut(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		writeError(w, http.StatusUnauthorized, "internal authentication failed")
		return
	}
	var policy firewall.ApplicationPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeError(w, http.StatusBadRequest, "invalid application firewall body")
		return
	}
	if policy.Application != r.PathValue("application") {
		writeError(w, http.StatusBadRequest, "application firewall path and body must match")
		return
	}
	status, err := h.bridge.ApplicationFirewallApply(r.Context(), policy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) authorized(r *http.Request) bool {
	return h.trustedSocket || internalauth.EqualToken(r.Header.Get(internalauth.HeaderName), h.internalToken)
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

func (h *Handler) wifiGet(w http.ResponseWriter, r *http.Request) {
	status, err := h.wifiClient.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) wifiEnable(w http.ResponseWriter, r *http.Request) {
	if h.wifiAPEnabled(r.Context()) {
		h.writeWifiClientAPConflict(w, r)
		return
	}
	status, err := h.wifiClient.Enable(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) wifiPut(w http.ResponseWriter, r *http.Request) {
	var req wificlient.ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid wifi apply body")
		return
	}
	if req.Desired && h.wifiAPEnabled(r.Context()) {
		h.writeWifiClientAPConflict(w, r)
		return
	}
	status, err := h.wifiClient.Apply(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) wifiScan(w http.ResponseWriter, r *http.Request) {
	result, err := h.wifiClient.Scan(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) wifiAPGet(w http.ResponseWriter, r *http.Request) {
	status, err := h.wifiAP.Status(r.Context())
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
	if req.Desired && h.wifiClientEnabled(r.Context()) {
		h.writeWifiAPClientConflict(w, r)
		return
	}
	status, err := h.wifiAP.Apply(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) wifiAPEnabled(ctx context.Context) bool {
	status, err := h.wifiAP.Status(ctx)
	return err == nil && (status.Desired || status.Actual == wifiap.ActualActive)
}

func (h *Handler) wifiClientEnabled(ctx context.Context) bool {
	status, err := h.wifiClient.Status(ctx)
	return err == nil && (status.Desired || status.Actual == wificlient.ActualActive || status.Actual == wificlient.ActualConnecting)
}

func (h *Handler) writeWifiClientAPConflict(w http.ResponseWriter, r *http.Request) {
	status, err := h.wifiClient.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status.Reason = wificlient.ReasonRadioInUseByAP
	status.Message = "client Wi-Fi cannot be enabled because Wi-Fi AP is already enabled. Disable Wi-Fi AP first."
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) writeWifiAPClientConflict(w http.ResponseWriter, r *http.Request) {
	status, err := h.wifiAP.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	status.Reason = wifiap.ReasonRadioInUse
	status.Message = "Wi-Fi AP cannot be enabled because client Wi-Fi is already enabled. Disable client Wi-Fi first."
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
