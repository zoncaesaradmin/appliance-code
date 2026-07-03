package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"appliance-code/server/backend/internal/keys"
	"appliance-code/server/backend/internal/registryauth"
	"appliance-code/server/backend/internal/reqauth"
	"appliance-code/server/backend/internal/storage"
	"appliance-code/server/backend/internal/users"
)

// RegistryTokenHandlers implements GET /api/v1/registry/token, the OCI
// Distribution token-service endpoint Podman, Skopeo, Buildah, Helm, and
// ORAS call automatically after zot's registry challenge.
type RegistryTokenHandlers struct {
	Auth       reqauth.Deps
	Users      *users.Service
	Authorizer *registryauth.Authorizer
	Keys       *keys.Material
	Issuer     string
}

type registryTokenResponse struct {
	Token     string    `json:"token"`
	ExpiresIn int       `json:"expires_in"`
	IssuedAt  time.Time `json:"issued_at"`
}

// Token authenticates the presented appliance API token (via HTTP Basic
// auth, per the OCI Distribution token contract — never an interactive
// password), intersects the requested scope with current RBAC and
// repository-prefix grants, and signs a five-minute registry access token.
func (h *RegistryTokenHandlers) Token(w http.ResponseWriter, r *http.Request) {
	_, password, ok := r.BasicAuth()
	if !ok || password == "" {
		w.Header().Set("WWW-Authenticate", `Basic realm="appliance"`)
		WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Basic auth with an appliance username and API token is required", "")
		return
	}

	principal, err := reqauth.Authenticate(r.Context(), h.Auth, password)
	if err != nil {
		if errors.Is(err, reqauth.ErrUnauthenticated) || errors.Is(err, reqauth.ErrInvalidCredential) {
			WriteProblem(w, r, http.StatusUnauthorized, "unauthorized", "Invalid or expired credential", "")
			return
		}
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	requests, err := registryauth.ParseScopes(r.URL.Query()["scope"])
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}

	user, err := h.Users.Get(r.Context(), principal.UserID)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	decisions, err := h.Authorizer.Authorize(r.Context(), principal.UserID, user.Username, principal.Permissions, requests)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	access := make([]registryauth.AccessEntry, len(decisions))
	for i, d := range decisions {
		granted := d.Granted
		if granted == nil {
			granted = []string{}
		}
		access[i] = registryauth.AccessEntry{Type: "repository", Name: d.Name, Actions: granted}
	}

	jti := uuid.Must(uuid.NewV7()).String()
	token, expiresAt, err := registryauth.IssueToken(h.Keys.RegistryPrivateKey, h.Keys.RegistryKeyID, h.Issuer, principal.UserID, "zot", jti, access)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	writeJSON(w, http.StatusOK, registryTokenResponse{
		Token: token, ExpiresIn: int(registryauth.TokenLifetime.Seconds()), IssuedAt: expiresAt.Add(-registryauth.TokenLifetime),
	})
}

// RegistryGrantHandlers implements the repository-prefix grant management
// HTTP surface.
type RegistryGrantHandlers struct {
	Grants storage.RegistryGrantStore
}

type registryGrantResponse struct {
	ID          string    `json:"id"`
	SubjectType string    `json:"subjectType"`
	SubjectID   string    `json:"subjectId"`
	PathPrefix  string    `json:"pathPrefix"`
	Actions     []string  `json:"actions"`
	CreatedAt   time.Time `json:"createdAt"`
}

func toRegistryGrantResponse(g storage.RegistryGrant) registryGrantResponse {
	return registryGrantResponse{
		ID: g.ID, SubjectType: string(g.SubjectType), SubjectID: g.SubjectID,
		PathPrefix: g.PathPrefix, Actions: g.Actions, CreatedAt: g.CreatedAt,
	}
}

func (h *RegistryGrantHandlers) List(w http.ResponseWriter, r *http.Request) {
	grants, err := h.Grants.List(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]registryGrantResponse, len(grants))
	for i, g := range grants {
		items[i] = toRegistryGrantResponse(g)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []registryGrantResponse `json:"items"`
	}{Items: items})
}

type createRegistryGrantRequest struct {
	SubjectType string   `json:"subjectType"`
	SubjectID   string   `json:"subjectId"`
	PathPrefix  string   `json:"pathPrefix"`
	Actions     []string `json:"actions"`
}

func (h *RegistryGrantHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createRegistryGrantRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}

	if req.SubjectType != string(storage.RegistryGrantSubjectUser) && req.SubjectType != string(storage.RegistryGrantSubjectRole) {
		WriteValidationProblem(w, r, `subjectType must be "user" or "role"`, nil)
		return
	}
	if req.SubjectID == "" {
		WriteValidationProblem(w, r, "subjectId is required", nil)
		return
	}
	prefix, err := registryauth.NormalizePathPrefix(req.PathPrefix)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	for _, a := range req.Actions {
		if a != "pull" && a != "push" {
			WriteValidationProblem(w, r, `actions must be a subset of "pull", "push"`, nil)
			return
		}
	}
	if len(req.Actions) == 0 {
		WriteValidationProblem(w, r, "at least one action is required", nil)
		return
	}

	grant := storage.RegistryGrant{
		ID: uuid.Must(uuid.NewV7()).String(), SubjectType: storage.RegistryGrantSubjectType(req.SubjectType),
		SubjectID: req.SubjectID, PathPrefix: prefix, Actions: req.Actions, CreatedAt: time.Now().UTC(),
	}
	if err := h.Grants.Create(r.Context(), grant); err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusCreated, toRegistryGrantResponse(grant))
}

func (h *RegistryGrantHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.Grants.Delete(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Registry grant not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
