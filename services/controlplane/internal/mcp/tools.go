package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/devflows"
	"appliance-code/services/controlplane/internal/reqauth"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolCallResult struct {
	Content           []toolContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func textToolResult(text string, structured any) toolCallResult {
	return toolCallResult{Content: []toolContent{{Type: "text", Text: text}}, StructuredContent: structured}
}

func (h *Handler) buildToolsEnabled() bool {
	return h.devflows != nil && h.capabilities.Enabled(appliance.CapabilityBuild)
}

type toolDefinition struct {
	name        string
	description string
	permissions []string
	inputSchema json.RawMessage
}

func mustInputSchema(schema map[string]any) json.RawMessage {
	raw, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	return raw
}

var emptyObjectSchema = mustInputSchema(map[string]any{
	"type":       "object",
	"properties": map[string]any{},
})

var buildToolDefinitions = []toolDefinition{
	{name: "list_work_profiles", description: "Return the available workspace profiles from the build catalog so a client can choose one before creating workspaces.", permissions: []string{roles.PermWorkProfilesRead}, inputSchema: emptyObjectSchema},
	{name: "list_workspaces", description: "List visible developer workspaces.", permissions: []string{roles.PermWorkspacesReadSelf, roles.PermWorkspacesReadAny}, inputSchema: emptyObjectSchema},
	{name: "get_workspace", description: "Get the current workspace, or one workspace when workspace_id is provided.", permissions: []string{roles.PermWorkspacesReadSelf, roles.PermWorkspacesReadAny}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workspace_id": map[string]any{"type": "string"},
		},
	})},
	{name: "create_workspace", description: "Create a workspace for one workspace profile. The profile configuration determines the repos available inside that workspace. The created workspace becomes the current workspace.", permissions: []string{roles.PermWorkspacesCreate}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workspace_name": map[string]any{"type": "string"},
			"profile_name":   map[string]any{"type": "string", "description": "Workspace profile name from list_work_profiles."},
		},
		"required": []string{"workspace_name", "profile_name"},
	})},
	{name: "set_workspace", description: "Set the current user-scoped workspace by workspace_id.", permissions: []string{roles.PermWorkspacesReadSelf}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workspace_id": map[string]any{"type": "string"},
		},
		"required": []string{"workspace_id"},
	})},
	{name: "delete_workspace", description: "Delete one workspace by workspace_id. Fails while that workspace has active jobs.", permissions: []string{roles.PermWorkspacesDeleteSelf, roles.PermWorkspacesDeleteAny}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"workspace_id": map[string]any{"type": "string"},
		},
		"required": []string{"workspace_id"},
	})},
	{name: "list_build_targets", description: "Return the allowed build target names for the current workspace.", permissions: []string{roles.PermBuildTargetsRead}, inputSchema: emptyObjectSchema},
	{name: "submit_build", description: "Submit an asynchronous build for the current workspace using one build target. If tag is omitted, a catalog-derived tag is generated.", permissions: []string{roles.PermBuildsCreate}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"build_target": map[string]any{"type": "string"},
			"tag":          map[string]any{"type": "string"},
		},
		"required": []string{"build_target"},
	})},
	{name: "list_jobs", description: "List developer workflow jobs.", permissions: []string{roles.PermJobsReadSelf, roles.PermJobsReadAny}, inputSchema: emptyObjectSchema},
	{name: "get_job_status", description: "Get one developer workflow job. If job_id is omitted, return the latest job for the current workspace.", permissions: []string{roles.PermJobsReadSelf, roles.PermJobsReadAny}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string"},
		},
	})},
	{name: "get_job_steps", description: "Get execution steps for one job.", permissions: []string{roles.PermJobsReadSelf, roles.PermJobsReadAny}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string"},
		},
		"required": []string{"job_id"},
	})},
	{name: "get_job_logs", description: "Get the aggregated logs for one job.", permissions: []string{roles.PermJobsReadSelf, roles.PermJobsReadAny}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string"},
		},
		"required": []string{"job_id"},
	})},
	{name: "cancel_job", description: "Cancel a queued or running job.", permissions: []string{roles.PermJobsCancelSelf, roles.PermJobsCancelAny}, inputSchema: mustInputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"job_id": map[string]any{"type": "string"},
		},
		"required": []string{"job_id"},
	})},
}

func (h *Handler) listTools(principal reqauth.Principal) []Tool {
	if !h.buildToolsEnabled() {
		return []Tool{}
	}
	tools := make([]Tool, 0, len(buildToolDefinitions))
	for _, def := range buildToolDefinitions {
		if !hasAnyPermission(principal.Permissions, def.permissions...) {
			continue
		}
		tools = append(tools, Tool{Name: def.name, Description: def.description, InputSchema: def.inputSchema})
	}
	return tools
}

func hasAnyPermission(perms map[string]bool, required ...string) bool {
	for _, perm := range required {
		if authz.HasPermission(perms, perm) {
			return true
		}
	}
	return false
}

func (h *Handler) handleToolCall(w http.ResponseWriter, r *http.Request, req Request, principal reqauth.Principal) {
	if !h.buildToolsEnabled() {
		writeJSONRPC(w, http.StatusOK, newError(req.ID, ErrCodeMethodNotFound, "Tool not found"))
		return
	}
	var params toolCallParams
	if len(req.Params) == 0 || json.Unmarshal(req.Params, &params) != nil || params.Name == "" {
		writeJSONRPC(w, http.StatusOK, newError(req.ID, ErrCodeInvalidParams, "Invalid params"))
		return
	}
	result, err := h.callTool(r, principal, params)
	if errors.Is(err, errToolNotFound) {
		writeJSONRPC(w, http.StatusOK, newError(req.ID, ErrCodeMethodNotFound, "Tool not found"))
		return
	}
	if errors.Is(err, errToolForbidden) {
		writeJSONRPC(w, http.StatusOK, newError(req.ID, ErrCodeInvalidRequest, "Permission denied"))
		return
	}
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, devflows.ErrNoCurrentWorkspace) {
		writeJSONRPC(w, http.StatusOK, newError(req.ID, ErrCodeInvalidRequest, err.Error()))
		return
	}
	if err != nil {
		writeJSONRPC(w, http.StatusOK, newError(req.ID, ErrCodeInternalError, err.Error()))
		return
	}
	writeJSONRPC(w, http.StatusOK, newResult(req.ID, result))
}

var (
	errToolNotFound  = errors.New("mcp: tool not found")
	errToolForbidden = errors.New("mcp: forbidden")
)

func (h *Handler) callTool(r *http.Request, principal reqauth.Principal, params toolCallParams) (toolCallResult, error) {
	need := func(perms ...string) error {
		if hasAnyPermission(principal.Permissions, perms...) {
			return nil
		}
		return errToolForbidden
	}
	actor := audit.Actor{Type: storage.AuditActorUser, UserID: principal.UserID, AuthMethod: principal.AuthMethod, RequestID: r.Header.Get("X-Request-Id"), SourceAddr: r.RemoteAddr}
	switch params.Name {
	case "list_work_profiles":
		if err := need(roles.PermWorkProfilesRead); err != nil {
			return toolCallResult{}, err
		}
		items := h.devflows.ListWorkProfiles(r.Context())
		return textToolResult(fmt.Sprintf("%d workspace profile(s)", len(items)), map[string]any{"items": items}), nil
	case "list_workspaces":
		if err := need(roles.PermWorkspacesReadSelf, roles.PermWorkspacesReadAny); err != nil {
			return toolCallResult{}, err
		}
		items, err := h.devflows.ListWorkspaces(r.Context(), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermWorkspacesReadAny))
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult(fmt.Sprintf("%d workspace(s)", len(items)), map[string]any{"items": workspacesForTool(items)}), nil
	case "get_workspace":
		if err := need(roles.PermWorkspacesReadSelf, roles.PermWorkspacesReadAny); err != nil {
			return toolCallResult{}, err
		}
		workspaceID, err := workspaceIDArg(params.Arguments, false)
		if err != nil {
			return toolCallResult{}, err
		}
		var ws storage.Workspace
		if workspaceID == "" {
			ws, err = h.devflows.CurrentWorkspace(r.Context(), principal.UserID)
		} else {
			ws, err = h.devflows.GetWorkspace(r.Context(), workspaceID, principal.UserID, authz.HasPermission(principal.Permissions, roles.PermWorkspacesReadAny))
		}
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult("workspace loaded", map[string]any{"workspace": workspaceForTool(ws)}), nil
	case "create_workspace":
		if err := need(roles.PermWorkspacesCreate); err != nil {
			return toolCallResult{}, err
		}
		var args struct {
			WorkspaceName string `json:"workspace_name"`
			ProfileName   string `json:"profile_name"`
			Name          string `json:"name"`
			WorkProfile   string `json:"workProfile"`
		}
		if json.Unmarshal(params.Arguments, &args) != nil {
			return toolCallResult{}, fmt.Errorf("invalid arguments")
		}
		workspaceName := firstNonEmpty(args.WorkspaceName, args.Name)
		profileName := firstNonEmpty(args.ProfileName, args.WorkProfile)
		if workspaceName == "" {
			return toolCallResult{}, fmt.Errorf("workspace_name is required")
		}
		if profileName == "" {
			return toolCallResult{}, fmt.Errorf("profile_name (workspace profile) is required")
		}
		ws, err := h.devflows.CreateWorkspace(r.Context(), actor, principal.UserID, devflows.CreateWorkspaceRequest{
			Name: workspaceName, WorkProfile: profileName,
		})
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult("workspace created", map[string]any{"workspace": workspaceForTool(ws)}), nil
	case "set_workspace":
		if err := need(roles.PermWorkspacesReadSelf); err != nil {
			return toolCallResult{}, err
		}
		workspaceID, err := workspaceIDArg(params.Arguments, true)
		if err != nil {
			return toolCallResult{}, err
		}
		ws, err := h.devflows.SetCurrentWorkspace(r.Context(), principal.UserID, workspaceID)
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult("current workspace set", map[string]any{"workspace": workspaceForTool(ws)}), nil
	case "delete_workspace":
		if err := need(roles.PermWorkspacesDeleteSelf, roles.PermWorkspacesDeleteAny); err != nil {
			return toolCallResult{}, err
		}
		workspaceID, err := workspaceIDArg(params.Arguments, true)
		if err != nil {
			return toolCallResult{}, err
		}
		err = h.devflows.DeleteWorkspace(r.Context(), actor, workspaceID, principal.UserID, authz.HasPermission(principal.Permissions, roles.PermWorkspacesDeleteAny))
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult("workspace deleted", map[string]any{"workspace_id": workspaceID}), nil
	case "list_build_targets":
		if err := need(roles.PermBuildTargetsRead); err != nil {
			return toolCallResult{}, err
		}
		items, err := h.devflows.ListBuildTargetsForCurrent(r.Context(), principal.UserID)
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult(fmt.Sprintf("%d build target(s)", len(items)), map[string]any{"items": items}), nil
	case "submit_build":
		if err := need(roles.PermBuildsCreate); err != nil {
			return toolCallResult{}, err
		}
		var args struct {
			BuildTarget string `json:"build_target"`
			Tag         string `json:"tag"`
			TargetName  string `json:"targetName"`
			ImageTag    string `json:"imageTag"`
		}
		if json.Unmarshal(params.Arguments, &args) != nil {
			return toolCallResult{}, fmt.Errorf("invalid arguments")
		}
		targetName := firstNonEmpty(args.BuildTarget, args.TargetName)
		if targetName == "" {
			return toolCallResult{}, fmt.Errorf("build_target is required")
		}
		job, err := h.devflows.SubmitBuildForCurrent(r.Context(), actor, principal.UserID, devflows.SubmitBuildRequest{TargetName: targetName, ImageTag: firstNonEmpty(args.Tag, args.ImageTag)}, "")
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult("build submitted", map[string]any{"job": jobForTool(job)}), nil
	case "list_jobs":
		if err := need(roles.PermJobsReadSelf, roles.PermJobsReadAny); err != nil {
			return toolCallResult{}, err
		}
		items, err := h.devflows.ListJobs(r.Context(), principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsReadAny))
		if err != nil {
			return toolCallResult{}, err
		}
		out := make([]map[string]any, len(items))
		for i, job := range items {
			out[i] = jobForTool(job)
		}
		return textToolResult(fmt.Sprintf("%d job(s)", len(items)), map[string]any{"items": out}), nil
	case "get_job_status":
		if err := need(roles.PermJobsReadSelf, roles.PermJobsReadAny); err != nil {
			return toolCallResult{}, err
		}
		jobID, err := jobIDArg(params.Arguments, false)
		if err != nil {
			return toolCallResult{}, err
		}
		var job storage.Job
		if jobID == "" {
			job, err = h.devflows.CurrentWorkspaceBuildStatus(r.Context(), principal.UserID)
		} else {
			job, err = h.devflows.GetJob(r.Context(), jobID, principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsReadAny))
		}
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult("job loaded", map[string]any{"job": jobForTool(job)}), nil
	case "get_job_steps":
		if err := need(roles.PermJobsReadSelf, roles.PermJobsReadAny); err != nil {
			return toolCallResult{}, err
		}
		jobID, err := jobIDArg(params.Arguments, true)
		if err != nil {
			return toolCallResult{}, err
		}
		steps, err := h.devflows.JobSteps(r.Context(), jobID, principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsReadAny))
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult(fmt.Sprintf("%d step(s)", len(steps)), map[string]any{"items": steps}), nil
	case "get_job_logs":
		if err := need(roles.PermJobsReadSelf, roles.PermJobsReadAny); err != nil {
			return toolCallResult{}, err
		}
		jobID, err := jobIDArg(params.Arguments, true)
		if err != nil {
			return toolCallResult{}, err
		}
		logs, err := h.devflows.JobLogs(r.Context(), jobID, principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsReadAny))
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult(logs, map[string]any{"logs": logs}), nil
	case "cancel_job":
		if err := need(roles.PermJobsCancelSelf, roles.PermJobsCancelAny); err != nil {
			return toolCallResult{}, err
		}
		jobID, err := jobIDArg(params.Arguments, true)
		if err != nil {
			return toolCallResult{}, err
		}
		job, err := h.devflows.CancelJob(r.Context(), actor, jobID, principal.UserID, authz.HasPermission(principal.Permissions, roles.PermJobsCancelAny))
		if err != nil {
			return toolCallResult{}, err
		}
		return textToolResult("job cancelled", map[string]any{"job": jobForTool(job)}), nil
	default:
		return toolCallResult{}, errToolNotFound
	}
}

func jobIDArg(raw json.RawMessage, required bool) (string, error) {
	var args struct {
		JobIDSnake string `json:"job_id"`
		JobIDCamel string `json:"jobId"`
	}
	if len(raw) != 0 && json.Unmarshal(raw, &args) != nil {
		return "", fmt.Errorf("invalid arguments")
	}
	jobID := firstNonEmpty(args.JobIDSnake, args.JobIDCamel)
	if required && jobID == "" {
		return "", fmt.Errorf("job_id is required")
	}
	return jobID, nil
}

func workspaceIDArg(raw json.RawMessage, required bool) (string, error) {
	var args struct {
		WorkspaceIDSnake string `json:"workspace_id"`
		WorkspaceIDCamel string `json:"workspaceId"`
	}
	if len(raw) != 0 && json.Unmarshal(raw, &args) != nil {
		return "", fmt.Errorf("invalid arguments")
	}
	workspaceID := firstNonEmpty(args.WorkspaceIDSnake, args.WorkspaceIDCamel)
	if required && workspaceID == "" {
		return "", fmt.Errorf("workspace_id is required")
	}
	return workspaceID, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func workspaceForTool(ws storage.Workspace) map[string]any {
	return map[string]any{"id": ws.ID, "ownerId": ws.OwnerID, "name": ws.Name, "workProfile": ws.WorkProfile, "sourceRepoUrl": ws.SourceRepoURL, "sourceRef": ws.SourceRef, "status": ws.Status}
}

func workspacesForTool(items []storage.Workspace) []map[string]any {
	out := make([]map[string]any, len(items))
	for i, ws := range items {
		out[i] = workspaceForTool(ws)
	}
	return out
}

func jobForTool(job storage.Job) map[string]any {
	return map[string]any{"id": job.ID, "ownerId": job.OwnerID, "workspaceId": job.WorkspaceID, "buildId": job.BuildID, "type": job.Type, "status": job.Status, "targetName": job.TargetName, "artifactRef": job.ArtifactRef, "reasonCode": job.ReasonCode, "errorMessage": job.ErrorMessage}
}
