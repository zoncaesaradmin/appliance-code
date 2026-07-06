package httpapi

import (
	"errors"
	"net/http"
	"time"

	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

// BuildHandlers implements the Build Management HTTP surface.
type BuildHandlers struct {
	Builds *builds.Service
}

type buildResponse struct {
	ID                 string     `json:"id"`
	OwnerID            string     `json:"ownerId"`
	Status             string     `json:"status"`
	SourceRepoURL      string     `json:"sourceRepoUrl"`
	SourceCommitSHA    string     `json:"sourceCommitSha"`
	ContainerfilePath  string     `json:"containerfilePath"`
	ImageRepository    string     `json:"imageRepository"`
	ImageTag           string     `json:"imageTag"`
	BuilderImageDigest string     `json:"builderImageDigest"`
	ReasonCode         string     `json:"reasonCode,omitempty"`
	ErrorMessage       string     `json:"errorMessage,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	StartedAt          *time.Time `json:"startedAt,omitempty"`
	CompletedAt        *time.Time `json:"completedAt,omitempty"`
	DeadlineAt         time.Time  `json:"deadlineAt"`
}

func toBuildResponse(b storage.Build) buildResponse {
	return buildResponse{
		ID: b.ID, OwnerID: b.OwnerID, Status: string(b.Status),
		SourceRepoURL: b.SourceRepoURL, SourceCommitSHA: b.SourceCommitSHA, ContainerfilePath: b.ContainerfilePath,
		ImageRepository: b.ImageRepository, ImageTag: b.ImageTag, BuilderImageDigest: b.BuilderImageDigest,
		ReasonCode: b.ReasonCode, ErrorMessage: b.ErrorMessage,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, StartedAt: b.StartedAt, CompletedAt: b.CompletedAt, DeadlineAt: b.DeadlineAt,
	}
}

// canReadOrCancel reports whether principal may act on a build it does not
// own, given the ".any" permission for the operation in question.
func canActOnAny(principal Principal, anyPermission string) bool {
	return authz.HasPermission(principal.Permissions, anyPermission)
}

type createBuildRequest struct {
	SourceRepoURL      string `json:"sourceRepoUrl"`
	SourceCommitSHA    string `json:"sourceCommitSha"`
	ContainerfilePath  string `json:"containerfilePath"`
	ImageRepository    string `json:"imageRepository"`
	ImageTag           string `json:"imageTag"`
	BuilderImageDigest string `json:"builderImageDigest"`
}

func (h *BuildHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req createBuildRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}

	principal, _ := PrincipalFromContext(r.Context())
	idempotencyKey := r.Header.Get("Idempotency-Key")

	build, err := h.Builds.Create(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), principal.UserID, builds.CreateRequest{
		SourceRepoURL: req.SourceRepoURL, SourceCommitSHA: req.SourceCommitSHA, ContainerfilePath: req.ContainerfilePath,
		ImageRepository: req.ImageRepository, ImageTag: req.ImageTag, BuilderImageDigest: req.BuilderImageDigest,
	}, idempotencyKey)

	switch {
	case errors.Is(err, builds.ErrIdempotencyKeyReused):
		WriteProblem(w, r, http.StatusConflict, "idempotency_key_reused", err.Error(), "")
		return
	case errors.Is(err, builds.ErrIdempotencyInProgress):
		WriteProblem(w, r, http.StatusConflict, "idempotency_in_progress", err.Error(), "")
		return
	case err != nil:
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusCreated, toBuildResponse(build))
}

func (h *BuildHandlers) List(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	filter := storage.BuildFilter{}
	if !canActOnAny(principal, roles.PermBuildsReadAny) {
		filter.OwnerID = principal.UserID
	}

	list, err := h.Builds.List(r.Context(), filter)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	items := make([]buildResponse, len(list))
	for i, b := range list {
		items[i] = toBuildResponse(b)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []buildResponse `json:"items"`
	}{Items: items})
}

// getOwnedBuild fetches a build and enforces the self/any ownership split,
// returning 404 rather than 403 for a build the caller cannot see, so a
// probing caller can't distinguish "doesn't exist" from "not yours."
func (h *BuildHandlers) getOwnedBuild(w http.ResponseWriter, r *http.Request, anyPermission string) (storage.Build, bool) {
	principal, _ := PrincipalFromContext(r.Context())
	build, err := h.Builds.Get(r.Context(), r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Build not found", "")
		return storage.Build{}, false
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return storage.Build{}, false
	}
	if build.OwnerID != principal.UserID && !canActOnAny(principal, anyPermission) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Build not found", "")
		return storage.Build{}, false
	}
	return build, true
}

func (h *BuildHandlers) Get(w http.ResponseWriter, r *http.Request) {
	build, ok := h.getOwnedBuild(w, r, roles.PermBuildsReadAny)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toBuildResponse(build))
}

func (h *BuildHandlers) Cancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.getOwnedBuild(w, r, roles.PermBuildsCancelAny); !ok {
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	result, err := h.Builds.Cancel(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("id"))
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, toBuildResponse(result))
}

func (h *BuildHandlers) Logs(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.getOwnedBuild(w, r, roles.PermBuildsReadAny); !ok {
		return
	}
	logs, err := h.Builds.Logs(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(logs))
}
