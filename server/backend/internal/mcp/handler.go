package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"appliance-code/server/backend/internal/authz"
	"appliance-code/server/backend/internal/reqauth"
	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/version"
)

const (
	// SessionIDHeader carries the server-assigned session identifier the
	// client must echo on every request after "initialize".
	SessionIDHeader = "Mcp-Session-Id"

	// maxBodyBytes bounds one JSON-RPC message independently of the REST
	// API's own body limit, per the plan's "independent request ... limits
	// on /mcp" requirement. MCP control messages are small; this is
	// deliberately far below the REST 1 MiB default.
	maxBodyBytes = 256 * 1024

	// defaultMaxSessions and defaultMaxConcurrentRequests are /mcp's own
	// session and concurrency limits, independent of any REST-side limits.
	defaultMaxSessions           = 1024
	defaultMaxConcurrentRequests = 64
)

// Handler implements the pinned MCP Streamable HTTP transport at one route.
type Handler struct {
	auth          reqauth.Deps
	sessions      *sessionStore
	allowedOrigin string // "scheme://host", or "" to skip Origin validation
	serverInfo    ServerInfo
	concurrency   chan struct{}
}

// NewHandler builds a Handler. canonicalOrigin is the appliance's
// configured external origin; requests carrying a different non-empty
// Origin header are rejected, per the plan's Origin-validation requirement.
func NewHandler(deps reqauth.Deps, canonicalOrigin string) *Handler {
	allowed := ""
	if u, err := url.Parse(canonicalOrigin); err == nil && u.Scheme != "" && u.Host != "" {
		allowed = u.Scheme + "://" + u.Host
	}
	return &Handler{
		auth:          deps,
		sessions:      newSessionStore(defaultMaxSessions),
		allowedOrigin: allowed,
		serverInfo:    ServerInfo{Name: "appliance-server", Version: version.Current().Version},
		concurrency:   make(chan struct{}, defaultMaxConcurrentRequests),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.originAllowed(r.Header.Get("Origin")) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	switch r.Method {
	case http.MethodPost:
		h.handlePost(w, r)
	case http.MethodGet:
		// Server-initiated push via a standalone SSE stream is not enabled
		// in v1; this is an explicitly spec-permitted way to decline it.
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "server-initiated streams are not supported", http.StatusMethodNotAllowed)
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) originAllowed(origin string) bool {
	if origin == "" || h.allowedOrigin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return u.Scheme+"://"+u.Host == h.allowedOrigin
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get(SessionIDHeader)
	if sessionID == "" {
		http.Error(w, "Mcp-Session-Id header required", http.StatusBadRequest)
		return
	}
	if _, err := reqauth.Authenticate(r.Context(), h.auth, bearer(r)); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !h.sessions.delete(sessionID) {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func bearer(r *http.Request) string {
	raw, _ := reqauth.BearerToken(r.Header.Get("Authorization"))
	return raw
}

func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request) {
	select {
	case h.concurrency <- struct{}{}:
		defer func() { <-h.concurrency }()
	default:
		http.Error(w, "too many concurrent MCP requests", http.StatusServiceUnavailable)
		return
	}

	principal, err := reqauth.Authenticate(r.Context(), h.auth, bearer(r))
	if err != nil {
		if errors.Is(err, reqauth.ErrUnauthenticated) || errors.Is(err, reqauth.ErrInvalidCredential) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "error reading request body", http.StatusBadRequest)
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPC(w, http.StatusBadRequest, newError(nil, ErrCodeParseError, "Parse error"))
		return
	}
	if req.JSONRPC != JSONRPCVersion || req.Method == "" {
		writeJSONRPC(w, http.StatusBadRequest, newError(req.ID, ErrCodeInvalidRequest, "Invalid Request"))
		return
	}

	sessionID := r.Header.Get(SessionIDHeader)

	if req.Method == "initialize" {
		h.handleInitialize(w, req, sessionID)
		return
	}

	if _, ok := h.sessions.get(sessionID); !ok {
		http.Error(w, "Mcp-Session-Id header required or unknown", http.StatusBadRequest)
		return
	}
	h.sessions.touch(sessionID)

	switch req.Method {
	case "notifications/initialized":
		h.sessions.markInitialized(sessionID)
		w.WriteHeader(http.StatusAccepted)
	case "ping":
		writeJSONRPC(w, http.StatusOK, newResult(req.ID, struct{}{}))
	case "tools/list":
		if !authz.HasPermission(principal.Permissions, roles.PermMCPInvoke) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		writeJSONRPC(w, http.StatusOK, newResult(req.ID, ToolsListResult{Tools: []Tool{}}))
	default:
		if req.IsNotification() {
			// Unknown notifications have no response to send; the client
			// isn't waiting for one.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSONRPC(w, http.StatusOK, newError(req.ID, ErrCodeMethodNotFound, "Method not found"))
	}
}

func (h *Handler) handleInitialize(w http.ResponseWriter, req Request, existingSessionID string) {
	if existingSessionID != "" {
		http.Error(w, "initialize must not carry an existing Mcp-Session-Id", http.StatusBadRequest)
		return
	}

	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeJSONRPC(w, http.StatusBadRequest, newError(req.ID, ErrCodeInvalidParams, "Invalid params"))
			return
		}
	}

	negotiated := ProtocolVersion // v1 pins exactly one revision; see the plan's MCP transport scope.

	sess, err := h.sessions.create(negotiated)
	if err != nil {
		http.Error(w, "server has reached its MCP session capacity", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set(SessionIDHeader, sess.ID)
	writeJSONRPC(w, http.StatusOK, newResult(req.ID, InitializeResult{
		ProtocolVersion: negotiated,
		Capabilities:    ServerCapabilities{Tools: &ToolsCapability{}},
		ServerInfo:      h.serverInfo,
	}))
}

func writeJSONRPC(w http.ResponseWriter, status int, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}
