package automationruntimeapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/automationruntimeauth"
	"appliance-code/services/controlplane/internal/httpapi"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/metadatabundle"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/version"
)

type Deps struct {
	Logger        logging.Logger
	Metadata      *metadatabundle.Service
	InternalToken string
}

func NewMux(deps Deps) http.Handler {
	startup := &httpapi.StartupState{}
	startup.MarkStarted()
	mux := http.NewServeMux()
	httpapi.RegisterHealthRoutes(mux, deps.Logger, staticReady{}, startup)
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(version.Current())
	})
	h := handlers{metadata: deps.Metadata}
	protected := requireInternalAuth(strings.TrimSpace(deps.InternalToken))
	mux.Handle("GET /internal/v1/metadata-bundle", protected(http.HandlerFunc(h.status)))
	mux.Handle("POST /internal/v1/metadata-bundle/validate", protected(http.HandlerFunc(h.validate)))
	mux.Handle("POST /internal/v1/metadata-bundle/install", protected(http.HandlerFunc(h.install)))
	mux.Handle("POST /internal/v1/metadata-bundle/rollback", protected(http.HandlerFunc(h.rollback)))
	mux.Handle("POST /internal/v1/automations/{automationId}/invoke", protected(http.HandlerFunc(h.invoke)))
	return httpapi.Chain(httpapi.TraceID, httpapi.RequestID, httpapi.AccessLog(deps.Logger), httpapi.Recover(deps.Logger))(mux)
}

type staticReady struct{}

func (staticReady) Ready(context.Context) error { return nil }

func requireInternalAuth(wantToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if wantToken == "" || !automationruntimeauth.EqualToken(r.Header.Get(automationruntimeauth.HeaderName), wantToken) {
				writeProblem(w, http.StatusUnauthorized, "internal authentication failed")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type handlers struct {
	metadata *metadatabundle.Service
}

func (h handlers) status(w http.ResponseWriter, r *http.Request) {
	st, err := h.metadata.Status(r.Context())
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, err.Error())
		return
	}
	b, err := h.metadata.ActiveBundle(r.Context())
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": st, "bundle": b})
}

func (h handlers) validate(w http.ResponseWriter, r *http.Request) {
	path, sig, _, cleanup, err := readArchive(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cleanup()
	validation, bundle, err := h.metadata.ValidateArchive(r.Context(), path, sig)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"validation": validation, "bundle": bundle})
}

func (h handlers) install(w http.ResponseWriter, r *http.Request) {
	path, sig, actor, cleanup, err := readArchive(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	defer cleanup()
	st, validation, err := h.metadata.InstallArchive(r.Context(), actor, path, sig)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, metadatabundle.ErrInvalidArchive) || errors.Is(err, metadatabundle.ErrInvalidSig) {
			code = http.StatusConflict
		}
		writeProblem(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": st, "validation": validation})
}

func (h handlers) rollback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actor audit.Actor `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := h.metadata.Rollback(r.Context(), req.Actor)
	if err != nil {
		writeProblem(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (h handlers) invoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Actor audit.Actor     `json:"actor"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.metadata.InvokeAutomation(r.Context(), req.Actor, r.PathValue("automationId"), req.Input)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, metadatabundle.ErrNotFound) {
			code = http.StatusNotFound
		}
		writeProblem(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func readArchive(r *http.Request) (string, string, audit.Actor, func(), error) {
	cleanup := func() {}
	var actor audit.Actor
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		return "", "", actor, cleanup, err
	}
	sig := strings.TrimSpace(r.FormValue("signature"))
	if sig == "" {
		return "", "", actor, cleanup, errors.New("signature is required")
	}
	actor = audit.Actor{
		UserID:     r.FormValue("actorUserId"),
		Type:       storage.AuditActorUser,
		AuthMethod: r.FormValue("actorAuthMethod"),
	}
	file, _, err := r.FormFile("archive")
	if err != nil {
		return "", "", actor, cleanup, err
	}
	defer file.Close()
	tmp, err := os.CreateTemp("", "automation-runtime-upload-*.tar.zst")
	if err != nil {
		return "", "", actor, cleanup, err
	}
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", "", actor, cleanup, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", "", actor, cleanup, err
	}
	cleanup = func() { _ = os.Remove(tmp.Name()) }
	return tmp.Name(), sig, actor, cleanup, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]any{
		"title":  http.StatusText(status),
		"detail": detail,
	})
}
