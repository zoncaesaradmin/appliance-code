package httpapi

import (
	"errors"
	"net/http"
	"time"

	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

// DeveloperWorkflowHandlers implements the ForgeLine-compatible server-side
// developer workflow surface in appliance-native HTTP semantics.
type DeveloperWorkflowHandlers struct {
	Devflows *devflows.Service
}

// workProfileResponse keeps the ForgeLine-compatible wire name, but the
// product-facing meaning is a workspace profile, not an appliance profile.
type workProfileResponse struct {
	Name        string                    `json:"name"`
	Description string                    `json:"description,omitempty"`
	Repos       []workProfileRepoResponse `json:"repos,omitempty"`
}

type workProfileRepoResponse struct {
	Name             string `json:"name"`
	EnabledByDefault bool   `json:"enabledByDefault,omitempty"`
}

type workspaceResponse struct {
	ID            string     `json:"id"`
	OwnerID       string     `json:"ownerId"`
	Name          string     `json:"name"`
	WorkProfile   string     `json:"workProfile"`
	SourceRepoURL string     `json:"sourceRepoUrl"`
	SourceRef     string     `json:"sourceRef"`
	Status        string     `json:"status"`
	ReasonCode    string     `json:"reasonCode,omitempty"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	DeletedAt     *time.Time `json:"deletedAt,omitempty"`
}

type buildTargetResponse struct {
	Name              string   `json:"name"`
	Aliases           []string `json:"aliases,omitempty"`
	Description       string   `json:"description,omitempty"`
	Repo              string   `json:"repo"`
	Execution         string   `json:"execution"`
	ScriptPath        string   `json:"scriptPath,omitempty"`
	MakeTarget        string   `json:"makeTarget,omitempty"`
	ContainerfilePath string   `json:"containerfilePath"`
	ImageRepository   string   `json:"imageRepository"`
}

type jobResponse struct {
	ID           string     `json:"id"`
	OwnerID      string     `json:"ownerId"`
	WorkspaceID  string     `json:"workspaceId,omitempty"`
	BuildID      string     `json:"buildId,omitempty"`
	Type         string     `json:"type"`
	Status       string     `json:"status"`
	TargetName   string     `json:"targetName,omitempty"`
	ArtifactRef  string     `json:"artifactRef,omitempty"`
	ReasonCode   string     `json:"reasonCode,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

type jobStepResponse struct {
	ID          string     `json:"id"`
	JobID       string     `json:"jobId"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Message     string     `json:"message,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

func toWorkspaceResponse(ws storage.Workspace) workspaceResponse {
	return workspaceResponse{ID: ws.ID, OwnerID: ws.OwnerID, Name: ws.Name, WorkProfile: ws.WorkProfile, SourceRepoURL: ws.SourceRepoURL, SourceRef: ws.SourceRef,
		Status: string(ws.Status), ReasonCode: ws.ReasonCode, ErrorMessage: ws.ErrorMessage, CreatedAt: ws.CreatedAt, UpdatedAt: ws.UpdatedAt, DeletedAt: ws.DeletedAt}
}

func toBuildTargetResponse(t devflows.BuildTarget) buildTargetResponse {
	return buildTargetResponse{Name: t.Name, Aliases: t.Aliases, Description: t.Description, Repo: t.Repo, Execution: t.Execution,
		ScriptPath: t.ScriptPath, MakeTarget: t.MakeTarget, ContainerfilePath: t.ContainerfilePath, ImageRepository: t.ImageRepository}
}

func toJobResponse(job storage.Job) jobResponse {
	return jobResponse{ID: job.ID, OwnerID: job.OwnerID, WorkspaceID: job.WorkspaceID, BuildID: job.BuildID, Type: string(job.Type), Status: string(job.Status), TargetName: job.TargetName, ArtifactRef: job.ArtifactRef,
		ReasonCode: job.ReasonCode, ErrorMessage: job.ErrorMessage, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, StartedAt: job.StartedAt, CompletedAt: job.CompletedAt}
}

func toJobStepResponse(step storage.JobStep) jobStepResponse {
	return jobStepResponse{ID: step.ID, JobID: step.JobID, Name: step.Name, Status: string(step.Status), Message: step.Message, CreatedAt: step.CreatedAt, StartedAt: step.StartedAt, CompletedAt: step.CompletedAt}
}

func (h *DeveloperWorkflowHandlers) ListWorkProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := h.Devflows.ListWorkProfiles(r.Context())
	items := make([]workProfileResponse, len(profiles))
	for i, p := range profiles {
		repos := make([]workProfileRepoResponse, len(p.Repos))
		for j, repo := range p.Repos {
			repos[j] = workProfileRepoResponse{Name: repo.Name, EnabledByDefault: repo.EnabledByDefault}
		}
		items[i] = workProfileResponse{Name: p.Name, Description: p.Description, Repos: repos}
	}
	writeJSON(w, http.StatusOK, struct {
		Items []workProfileResponse `json:"items"`
	}{Items: items})
}

type createWorkspaceRequest struct {
	Name        string `json:"name"`
	WorkProfile string `json:"workProfile"`
}

func (h *DeveloperWorkflowHandlers) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req createWorkspaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	ws, err := h.Devflows.CreateWorkspace(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), principal.UserID, devflows.CreateWorkspaceRequest{Name: req.Name, WorkProfile: req.WorkProfile})
	if err != nil {
		WriteValidationProblem(w, r, err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, toWorkspaceResponse(ws))
}

func (h *DeveloperWorkflowHandlers) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	items, err := h.Devflows.ListWorkspaces(r.Context(), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermWorkspacesReadAny))
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	out := make([]workspaceResponse, len(items))
	for i, ws := range items {
		out[i] = toWorkspaceResponse(ws)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []workspaceResponse `json:"items"`
	}{Items: out})
}

func (h *DeveloperWorkflowHandlers) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	ws, err := h.Devflows.GetWorkspace(r.Context(), r.PathValue("workspaceId"), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermWorkspacesReadAny))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Workspace not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, toWorkspaceResponse(ws))
}

func (h *DeveloperWorkflowHandlers) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	err := h.Devflows.DeleteWorkspace(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("workspaceId"), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermWorkspacesDeleteAny))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Workspace not found", "")
		return
	}
	if errors.Is(err, devflows.ErrWorkspaceHasActiveJobs) {
		WriteProblem(w, r, http.StatusConflict, "workspace_has_active_jobs", "Workspace has active jobs", "Cancel or wait for active jobs before deleting this workspace.")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *DeveloperWorkflowHandlers) GetCurrentWorkspace(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	ws, err := h.Devflows.CurrentWorkspace(r.Context(), principal.UserID)
	if errors.Is(err, devflows.ErrNoCurrentWorkspace) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Current workspace not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, toWorkspaceResponse(ws))
}

type setCurrentWorkspaceRequest struct {
	WorkspaceID string `json:"workspaceId"`
}

func (h *DeveloperWorkflowHandlers) SetCurrentWorkspace(w http.ResponseWriter, r *http.Request) {
	var req setCurrentWorkspaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	ws, err := h.Devflows.SetCurrentWorkspace(r.Context(), principal.UserID, req.WorkspaceID)
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Workspace not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, toWorkspaceResponse(ws))
}

func (h *DeveloperWorkflowHandlers) ListCurrentBuildTargets(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	targets, err := h.Devflows.ListBuildTargetsForCurrent(r.Context(), principal.UserID)
	if errors.Is(err, devflows.ErrNoCurrentWorkspace) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Current workspace not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	out := make([]buildTargetResponse, len(targets))
	for i, target := range targets {
		out[i] = toBuildTargetResponse(target)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []buildTargetResponse `json:"items"`
	}{Items: out})
}

type submitCurrentBuildRequest struct {
	TargetName string `json:"targetName"`
	ImageTag   string `json:"imageTag"`
}

func (h *DeveloperWorkflowHandlers) SubmitCurrentBuild(w http.ResponseWriter, r *http.Request) {
	var req submitCurrentBuildRequest
	if err := decodeJSON(w, r, &req); err != nil {
		WriteValidationProblem(w, r, "invalid request body", nil)
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	job, err := h.Devflows.SubmitBuildForCurrent(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), principal.UserID, devflows.SubmitBuildRequest{TargetName: req.TargetName, ImageTag: req.ImageTag}, r.Header.Get("Idempotency-Key"))
	switch {
	case errors.Is(err, devflows.ErrNoCurrentWorkspace):
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Current workspace not found", "")
	case errors.Is(err, builds.ErrIdempotencyKeyReused):
		WriteProblem(w, r, http.StatusConflict, "idempotency_key_reused", err.Error(), "")
	case errors.Is(err, builds.ErrIdempotencyInProgress):
		WriteProblem(w, r, http.StatusConflict, "idempotency_in_progress", err.Error(), "")
	case err != nil:
		WriteValidationProblem(w, r, err.Error(), nil)
	default:
		writeJSON(w, http.StatusCreated, toJobResponse(job))
	}
}

func (h *DeveloperWorkflowHandlers) CurrentWorkspaceBuildStatus(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	job, err := h.Devflows.CurrentWorkspaceBuildStatus(r.Context(), principal.UserID)
	switch {
	case errors.Is(err, devflows.ErrNoCurrentWorkspace):
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Current workspace not found", "")
	case errors.Is(err, storage.ErrNotFound):
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Current workspace build status not found", "")
	case err != nil:
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
	default:
		writeJSON(w, http.StatusOK, toJobResponse(job))
	}
}

func (h *DeveloperWorkflowHandlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	items, err := h.Devflows.ListJobs(r.Context(), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsReadAny))
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	out := make([]jobResponse, len(items))
	for i, job := range items {
		out[i] = toJobResponse(job)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []jobResponse `json:"items"`
	}{Items: out})
}

func (h *DeveloperWorkflowHandlers) GetJob(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	job, err := h.Devflows.GetJob(r.Context(), r.PathValue("jobId"), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsReadAny))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Job not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, toJobResponse(job))
}

func (h *DeveloperWorkflowHandlers) CancelJob(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	job, err := h.Devflows.CancelJob(r.Context(), principal.Actor(requestIDFromRequest(r), r.RemoteAddr), r.PathValue("jobId"), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsCancelAny))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Job not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, toJobResponse(job))
}

func (h *DeveloperWorkflowHandlers) JobSteps(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	steps, err := h.Devflows.JobSteps(r.Context(), r.PathValue("jobId"), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsReadAny))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Job not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	out := make([]jobStepResponse, len(steps))
	for i, step := range steps {
		out[i] = toJobStepResponse(step)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []jobStepResponse `json:"items"`
	}{Items: out})
}

func (h *DeveloperWorkflowHandlers) JobLogs(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	logs, err := h.Devflows.JobLogs(r.Context(), r.PathValue("jobId"), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsReadAny))
	if errors.Is(err, storage.ErrNotFound) {
		WriteProblem(w, r, http.StatusNotFound, "not_found", "Job not found", "")
		return
	}
	if err != nil {
		WriteProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal server error", "")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(logs))
}
