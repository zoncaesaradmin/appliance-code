package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"appliance-code/server/backend/internal/registryauth"
	"appliance-code/server/backend/internal/users"
	"appliance-code/server/backend/internal/zotadapter"
)

// RegistryCatalogHandlers implements the read-only repository/tag/referrer
// catalog HTTP surface. zot remains authoritative for this data; these
// handlers only reconcile it through zotadapter and filter it by the
// caller's registry grants before returning it, per the plan's "search must
// filter unauthorized repositories" requirement.
type RegistryCatalogHandlers struct {
	Zot        zotadapter.Client
	Authorizer *registryauth.Authorizer
	Users      *users.Service
}

func (h *RegistryCatalogHandlers) callerIdentity(r *http.Request) (userID, username string, ok bool) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		return "", "", false
	}
	user, err := h.Users.Get(r.Context(), principal.UserID)
	if err != nil {
		return "", "", false
	}
	return principal.UserID, user.Username, true
}

func (h *RegistryCatalogHandlers) ListRepositories(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	userID, username, ok := h.callerIdentity(r)
	if !ok {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}

	all, err := h.Zot.ListRepositories(r.Context())
	if err != nil {
		WriteProblem(w, r, http.StatusBadGateway, "registry_unavailable", "The OCI registry data plane is unavailable", "")
		return
	}

	visible, err := h.Authorizer.FilterPullable(r.Context(), userID, username, principal.Permissions, all)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items []string `json:"items"`
	}{Items: visible})
}

func (h *RegistryCatalogHandlers) checkRepositoryPullable(w http.ResponseWriter, r *http.Request, repository string) bool {
	principal, _ := PrincipalFromContext(r.Context())
	userID, username, ok := h.callerIdentity(r)
	if !ok {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return false
	}
	normalized, err := registryauth.NormalizeRepositoryName(repository)
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return false
	}
	canPull, err := h.Authorizer.CanPull(r.Context(), userID, username, principal.Permissions, normalized)
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return false
	}
	if !canPull {
		// A 404 rather than 403 avoids confirming a repository's existence
		// to a caller who cannot read it.
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Repository not found", "")
		return false
	}
	return true
}

// CatalogItem dispatches GET /api/v1/registry/repositories/{rest...},
// matching either "<repository>/tags" or "<repository>/referrers". A
// single wildcard route is required because repository names are
// themselves multi-segment paths (e.g. "users/alice/app"), which the
// stdlib mux cannot express as a fixed {name} segment followed by a
// literal suffix.
func (h *RegistryCatalogHandlers) CatalogItem(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("rest")
	switch {
	case strings.HasSuffix(rest, "/tags"):
		h.listTagsFor(w, r, strings.TrimSuffix(rest, "/tags"))
	case strings.HasSuffix(rest, "/referrers"):
		h.listReferrersFor(w, r, strings.TrimSuffix(rest, "/referrers"))
	default:
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Not found", "")
	}
}

func (h *RegistryCatalogHandlers) listTagsFor(w http.ResponseWriter, r *http.Request, repository string) {
	if !h.checkRepositoryPullable(w, r, repository) {
		return
	}

	tags, err := h.Zot.ListTags(r.Context(), repository)
	if errors.Is(err, zotadapter.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Repository not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusBadGateway, "registry_unavailable", "The OCI registry data plane is unavailable", "")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items []string `json:"items"`
	}{Items: tags})
}

func (h *RegistryCatalogHandlers) listReferrersFor(w http.ResponseWriter, r *http.Request, repository string) {
	if !h.checkRepositoryPullable(w, r, repository) {
		return
	}

	digest := r.URL.Query().Get("digest")
	if digest == "" {
		WriteValidationProblem(w, r, "digest query parameter is required", nil)
		return
	}

	referrers, err := h.Zot.ListReferrers(r.Context(), repository, digest)
	if err != nil {
		WriteProblem(w, r, http.StatusBadGateway, "registry_unavailable", "The OCI registry data plane is unavailable", "")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Items []zotadapterDescriptor `json:"items"`
	}{Items: toDescriptorResponses(referrers)})
}

// zotadapterDescriptor mirrors zotadapter.Descriptor for the JSON response,
// kept as a distinct type so the wire shape doesn't silently change if the
// adapter's internal type does.
type zotadapterDescriptor struct {
	MediaType    string `json:"mediaType"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
	ArtifactType string `json:"artifactType,omitempty"`
}

func toDescriptorResponses(descriptors []zotadapter.Descriptor) []zotadapterDescriptor {
	out := make([]zotadapterDescriptor, len(descriptors))
	for i, d := range descriptors {
		out[i] = zotadapterDescriptor{MediaType: d.MediaType, Digest: d.Digest, Size: d.Size, ArtifactType: d.ArtifactType}
	}
	return out
}
