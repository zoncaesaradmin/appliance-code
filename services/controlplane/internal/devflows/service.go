package devflows

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/buildergit"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/storage"
	"appliance-code/services/controlplane/internal/workflows"
)

var (
	ErrNoCurrentWorkspace       = errors.New("devflows: no current workspace selected")
	ErrForbidden                = errors.New("devflows: forbidden")
	ErrWorkspaceHasActiveJobs   = errors.New("devflows: workspace has active jobs")
	ErrWorkspaceNameConflict    = errors.New("devflows: workspace name already exists")
	ErrWorkspaceProfileConflict = errors.New("devflows: workspace name already exists on a different workspace profile")
	ErrWorkspaceNotReady        = errors.New("devflows: current workspace is not ready")
)

type Service struct {
	catalog            Catalog
	workspaces         storage.WorkspaceStore
	jobs               storage.JobStore
	builds             *builds.Service
	engine             workflows.Engine
	provisionerImage   string
	workspaceRootDir   string
	workspaceClaimName string
	builderGit         *buildergit.Service
	logger             logging.Logger
	audit              *audit.Recorder
}

func NewService(catalog Catalog, workspaces storage.WorkspaceStore, jobs storage.JobStore, buildsSvc *builds.Service, engine workflows.Engine, provisionerImage, workspaceRootDir, workspaceClaimName string, builderGit *buildergit.Service, logger logging.Logger, recorder *audit.Recorder) (*Service, error) {
	if logger == nil {
		return nil, errors.New("devflows: logger is required")
	}
	return &Service{
		catalog:            catalog,
		workspaces:         workspaces,
		jobs:               jobs,
		builds:             buildsSvc,
		engine:             engine,
		provisionerImage:   strings.TrimSpace(provisionerImage),
		workspaceRootDir:   strings.TrimSpace(workspaceRootDir),
		workspaceClaimName: strings.TrimSpace(workspaceClaimName),
		builderGit:         builderGit,
		logger:             logger,
		audit:              recorder,
	}, nil
}

func (s *Service) Catalog() Catalog { return s.catalog }

type CreateWorkspaceRequest struct {
	Name        string
	WorkProfile string
}

type SubmitBuildRequest struct {
	TargetName string
	ImageTag   string
}

func artifactRef(imageRepository, imageTag string) string {
	if imageRepository == "" || imageTag == "" {
		return ""
	}
	return imageRepository + ":" + imageTag
}

func (s *Service) ListWorkProfiles(_ context.Context) []WorkProfile {
	out := make([]WorkProfile, len(s.catalog.WorkProfiles))
	copy(out, s.catalog.WorkProfiles)
	return out
}

func (s *Service) CreateWorkspace(ctx context.Context, actor audit.Actor, ownerID string, req CreateWorkspaceRequest) (storage.Workspace, error) {
	name := normalizeName(req.Name)
	if !validName(name) {
		return storage.Workspace{}, fmt.Errorf("devflows: workspace name is invalid")
	}
	profile := normalizeName(req.WorkProfile)
	if profile == "" {
		profile = "default"
	}
	if _, ok := s.catalog.WorkProfile(profile); !ok {
		return storage.Workspace{}, fmt.Errorf("devflows: unknown workspace profile %q", req.WorkProfile)
	}
	repos, err := s.catalog.ReposForProfile(profile)
	if err != nil {
		return storage.Workspace{}, err
	}
	if len(repos) == 0 {
		return storage.Workspace{}, fmt.Errorf("devflows: workspace profile %q has no repos configured", profile)
	}
	gitCredentialSecret, err := s.resolveWorkspaceGitCredential(ctx, repos)
	if err != nil {
		return storage.Workspace{}, err
	}
	provisionerImage := s.provisionerImage
	if provisionerImage == "" {
		return storage.Workspace{}, fmt.Errorf("devflows: workspaceProvisionerImageDigest is required for workspace profile %q", profile)
	}
	existing, err := s.workspaces.List(ctx, storage.WorkspaceFilter{OwnerID: ownerID, Limit: 200})
	if err != nil {
		return storage.Workspace{}, err
	}
	for _, current := range existing {
		if normalizeName(current.Name) != name {
			continue
		}
		if normalizeName(current.WorkProfile) != profile {
			return storage.Workspace{}, ErrWorkspaceProfileConflict
		}
		return storage.Workspace{}, ErrWorkspaceNameConflict
	}

	now := time.Now().UTC()
	ws := storage.Workspace{
		ID:          uuid.Must(uuid.NewV7()).String(),
		OwnerID:     ownerID,
		Name:        name,
		WorkProfile: profile,
		Status:      storage.WorkspaceStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	job := storage.Job{
		ID:          uuid.Must(uuid.NewV7()).String(),
		OwnerID:     ownerID,
		WorkspaceID: ws.ID,
		Type:        storage.JobTypeWorkspacePrepare,
		Status:      storage.JobStatusQueued,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.workspaces.Create(ctx, ws); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			return storage.Workspace{}, ErrWorkspaceNameConflict
		}
		return storage.Workspace{}, err
	}
	if err := s.workspaces.SetCurrent(ctx, ownerID, ws.ID); err != nil {
		return storage.Workspace{}, err
	}
	if err := s.jobs.Create(ctx, job); err != nil {
		return storage.Workspace{}, err
	}
	_ = s.jobs.AddStep(ctx, storage.JobStep{
		ID:        uuid.Must(uuid.NewV7()).String(),
		JobID:     job.ID,
		Name:      "submit-workspace-provision-workflow",
		Status:    storage.JobStatusQueued,
		Message:   "queued workspace provisioning workflow",
		CreatedAt: now,
	})
	if s.audit != nil {
		_ = s.audit.Record(ctx, actor, audit.Event{Action: "workspaces.create", TargetType: "workspace", TargetID: ws.ID, Outcome: storage.AuditOutcomeSuccess})
	}

	return s.submitWorkspaceProvision(ctx, ws, job, repos, provisionerImage, gitCredentialSecret)
}

func (s *Service) submitWorkspaceProvision(ctx context.Context, ws storage.Workspace, job storage.Job, repos []Repo, imageDigest, gitCredentialSecret string) (storage.Workspace, error) {
	startedAt := time.Now().UTC()
	workflowName := workspacePrepareWorkflowName(job.ID)
	if s.engine == nil {
		reason := "workflow_engine_unavailable"
		message := "workspace workflow engine is not configured"
		_ = s.jobs.UpdateStatus(ctx, job.ID, storage.JobStatusFailed, reason, message, nil, &startedAt)
		_ = s.workspaces.UpdateStatus(ctx, ws.ID, storage.WorkspaceStatusFailed, reason, message)
		ws.Status = storage.WorkspaceStatusFailed
		ws.ReasonCode = reason
		ws.ErrorMessage = message
		ws.UpdatedAt = startedAt
		s.logger.WithContext(ctx).Errorw("workspace provisioning unavailable",
			"workspaceID", ws.ID,
			"workspaceName", ws.Name,
			"workProfile", ws.WorkProfile,
			"jobID", job.ID,
			"workflowName", workflowName,
			"reasonCode", reason,
			"errorMessage", message,
		)
		return ws, nil
	}

	submitErr := s.engine.Submit(ctx, workflows.Spec{
		Name:                workflowName,
		Kind:                workflows.KindWorkspacePrepare,
		BuilderImageDigest:  imageDigest,
		GitCredentialSecret: gitCredentialSecret,
		Deadline:            startedAt.Add(30 * time.Minute),
		WorkspaceRootDir:    s.workspaceRootDir,
		WorkspaceClaimName:  s.workspaceClaimName,
		WorkspaceName:       ws.Name,
		WorkspaceRepos:      workspaceRepoSpecs(repos),
	})
	completedAt := time.Now().UTC()
	if submitErr != nil {
		reason := "workflow_submit_failed"
		message := submitErr.Error()
		_ = s.jobs.UpdateStatus(ctx, job.ID, storage.JobStatusFailed, reason, message, nil, &completedAt)
		_ = s.workspaces.UpdateStatus(ctx, ws.ID, storage.WorkspaceStatusFailed, reason, message)
		ws.Status = storage.WorkspaceStatusFailed
		ws.ReasonCode = reason
		ws.ErrorMessage = message
		ws.UpdatedAt = completedAt
		s.logger.WithContext(ctx).Errorw("workspace provisioning workflow submission failed",
			"workspaceID", ws.ID,
			"workspaceName", ws.Name,
			"workProfile", ws.WorkProfile,
			"jobID", job.ID,
			"workflowName", workflowName,
			"repoNames", workspaceRepoNames(repos),
			"workspaceRootDir", s.workspaceRootDir,
			"workspaceClaimName", s.workspaceClaimName,
			"error", submitErr,
		)
		return ws, nil
	}
	_ = s.jobs.UpdateStatus(ctx, job.ID, storage.JobStatusRunning, "", "", &completedAt, nil)
	s.logger.WithContext(ctx).Infow("workspace provisioning workflow submitted",
		"workspaceID", ws.ID,
		"workspaceName", ws.Name,
		"workProfile", ws.WorkProfile,
		"jobID", job.ID,
		"workflowName", workflowName,
		"repoNames", workspaceRepoNames(repos),
		"workspaceRootDir", s.workspaceRootDir,
		"workspaceClaimName", s.workspaceClaimName,
	)
	return ws, nil
}

func (s *Service) resolveWorkspaceGitCredential(ctx context.Context, repos []Repo) (string, error) {
	if s.builderGit == nil {
		return "", nil
	}
	secretName := ""
	for _, repo := range repos {
		credential, err := s.builderGit.Resolve(ctx, repo.URL)
		if err != nil {
			return "", err
		}
		if secretName == "" {
			secretName = credential.SecretName
			continue
		}
		if credential.SecretName != secretName {
			return "", fmt.Errorf("devflows: workspace profile requires multiple Git credentials, which is not supported yet")
		}
	}
	return secretName, nil
}

func workspaceRepoSpecs(repos []Repo) []workflows.WorkspaceRepo {
	out := make([]workflows.WorkspaceRepo, 0, len(repos))
	for _, repo := range repos {
		out = append(out, workflows.WorkspaceRepo{Name: repo.Name, URL: repo.URL, Ref: repo.DefaultRef})
	}
	return out
}

func workspaceRepoNames(repos []Repo) []string {
	out := make([]string, 0, len(repos))
	for _, repo := range repos {
		out = append(out, repo.Name)
	}
	return out
}

func (s *Service) ListWorkspaces(ctx context.Context, ownerID string, canAny bool) ([]storage.Workspace, error) {
	filter := storage.WorkspaceFilter{}
	if !canAny {
		filter.OwnerID = ownerID
	}
	items, err := s.workspaces.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]storage.Workspace, len(items))
	for i, ws := range items {
		out[i], err = s.reconcileWorkspace(ctx, ws)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) GetWorkspace(ctx context.Context, id, ownerID string, canAny bool) (storage.Workspace, error) {
	ws, err := s.workspaces.Get(ctx, id)
	if err != nil {
		return storage.Workspace{}, err
	}
	if ws.OwnerID != ownerID && !canAny {
		return storage.Workspace{}, storage.ErrNotFound
	}
	return s.reconcileWorkspace(ctx, ws)
}

func (s *Service) DeleteWorkspace(ctx context.Context, actor audit.Actor, id, ownerID string, canAny bool) error {
	ws, err := s.GetWorkspace(ctx, id, ownerID, canAny)
	if err != nil {
		return err
	}
	jobs, err := s.jobs.List(ctx, storage.JobFilter{WorkspaceID: ws.ID, Limit: 200})
	if err != nil {
		return err
	}
	for _, job := range jobs {
		reconciled, err := s.reconcileJob(ctx, job)
		if err != nil {
			return err
		}
		if !reconciled.Status.Terminal() {
			return ErrWorkspaceHasActiveJobs
		}
	}
	if err := s.workspaces.MarkDeleted(ctx, ws.ID, time.Now().UTC()); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, actor, audit.Event{Action: "workspaces.delete", TargetType: "workspace", TargetID: ws.ID, Outcome: storage.AuditOutcomeSuccess})
	}
	return nil
}

func (s *Service) SetCurrentWorkspace(ctx context.Context, userID, workspaceID string) (storage.Workspace, error) {
	ws, err := s.GetWorkspace(ctx, workspaceID, userID, false)
	if err != nil {
		return storage.Workspace{}, err
	}
	if ws.DeletedAt != nil {
		return storage.Workspace{}, storage.ErrNotFound
	}
	if err := s.workspaces.SetCurrent(ctx, userID, workspaceID); err != nil {
		return storage.Workspace{}, err
	}
	return s.reconcileWorkspace(ctx, ws)
}

func (s *Service) CurrentWorkspace(ctx context.Context, userID string) (storage.Workspace, error) {
	cur, err := s.workspaces.GetCurrent(ctx, userID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Workspace{}, ErrNoCurrentWorkspace
	}
	if err != nil {
		return storage.Workspace{}, err
	}
	ws, err := s.workspaces.Get(ctx, cur.WorkspaceID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.Workspace{}, ErrNoCurrentWorkspace
	}
	if err != nil {
		return storage.Workspace{}, err
	}
	if ws.OwnerID != userID || ws.DeletedAt != nil {
		return storage.Workspace{}, ErrNoCurrentWorkspace
	}
	return s.reconcileWorkspace(ctx, ws)
}

func (s *Service) ListBuildTargetsForCurrent(ctx context.Context, userID string) ([]BuildTarget, error) {
	ws, err := s.CurrentWorkspace(ctx, userID)
	if err != nil {
		return nil, err
	}
	targets, err := s.catalog.TargetsForProfile(ws.WorkProfile)
	if err != nil {
		return nil, err
	}
	return targets, nil
}

func (s *Service) SubmitBuildForCurrent(ctx context.Context, actor audit.Actor, ownerID string, req SubmitBuildRequest, idempotencyKey string) (storage.Job, error) {
	ws, err := s.CurrentWorkspace(ctx, ownerID)
	if err != nil {
		return storage.Job{}, err
	}
	if ws.Status != storage.WorkspaceStatusReady {
		return storage.Job{}, ErrWorkspaceNotReady
	}
	resolved, err := s.catalog.ResolveTargetForProfile(ws.WorkProfile, req.TargetName)
	if err != nil {
		return storage.Job{}, err
	}
	sourceRef := strings.TrimSpace(resolved.Repo.DefaultRef)
	if !IsCommitSHA(sourceRef) {
		return storage.Job{}, fmt.Errorf("devflows: repo %q defaultRef %q is mutable; configure an immutable commit SHA in the builder catalog", resolved.Repo.Name, sourceRef)
	}
	tag := strings.TrimSpace(req.ImageTag)
	if tag == "" {
		tag = renderTag(resolved.Target.ImageTagTemplate, ws, resolved.Target, sourceRef)
	}
	if tag == "" {
		tag = sourceRef[:min(12, len(sourceRef))]
	}
	buildReq := builds.CreateRequest{SourceRepoURL: resolved.Repo.URL, SourceCommitSHA: sourceRef,
		Execution: resolved.Target.Execution, ScriptPath: resolved.Target.ScriptPath, MakeTarget: resolved.Target.MakeTarget,
		ContainerfilePath: resolved.Target.ContainerfilePath, ImageRepository: resolved.Target.ImageRepository, ImageTag: tag,
		BuilderImageDigest: resolved.Target.BuilderImageDigest}
	build, err := s.builds.Create(ctx, actor, ownerID, buildReq, idempotencyKey)
	if err != nil {
		return storage.Job{}, err
	}
	existingJobs, err := s.jobs.List(ctx, storage.JobFilter{OwnerID: ownerID, WorkspaceID: ws.ID, Type: storage.JobTypeBuild, Limit: 200})
	if err != nil {
		return storage.Job{}, err
	}
	for _, existing := range existingJobs {
		if existing.BuildID == build.ID {
			return s.reconcileJob(ctx, existing)
		}
	}
	now := time.Now().UTC()
	job := storage.Job{ID: uuid.Must(uuid.NewV7()).String(), OwnerID: ownerID, WorkspaceID: ws.ID, BuildID: build.ID, Type: storage.JobTypeBuild,
		Status: jobStatusFromBuild(build.Status), TargetName: resolved.Target.Name, ArtifactRef: artifactRef(build.ImageRepository, build.ImageTag),
		CreatedAt: now, UpdatedAt: now, StartedAt: build.StartedAt, CompletedAt: build.CompletedAt,
		ReasonCode: build.ReasonCode, ErrorMessage: build.ErrorMessage}
	if err := s.jobs.Create(ctx, job); err != nil {
		return storage.Job{}, err
	}
	_ = s.jobs.AddStep(ctx, storage.JobStep{ID: uuid.Must(uuid.NewV7()).String(), JobID: job.ID, Name: "submit-build-workflow", Status: job.Status, Message: "submitted build workflow", CreatedAt: now, StartedAt: build.StartedAt, CompletedAt: build.CompletedAt})
	return job, nil
}

func (s *Service) CurrentWorkspaceBuildStatus(ctx context.Context, userID string) (storage.Job, error) {
	ws, err := s.CurrentWorkspace(ctx, userID)
	if err != nil {
		return storage.Job{}, err
	}
	jobs, err := s.jobs.List(ctx, storage.JobFilter{OwnerID: userID, WorkspaceID: ws.ID, Type: storage.JobTypeBuild, Limit: 1})
	if err != nil {
		return storage.Job{}, err
	}
	if len(jobs) == 0 {
		return storage.Job{}, storage.ErrNotFound
	}
	return s.reconcileJob(ctx, jobs[0])
}

func renderTag(tmpl string, ws storage.Workspace, target BuildTarget, sourceRef string) string {
	if strings.TrimSpace(tmpl) == "" {
		return ""
	}
	return strings.NewReplacer("{workspace}", ws.Name, "{target}", target.Name, "{commit12}", sourceRef[:min(12, len(sourceRef))]).Replace(tmpl)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func jobStatusFromBuild(status storage.BuildStatus) storage.JobStatus {
	switch status {
	case storage.BuildStatusSucceeded:
		return storage.JobStatusSucceeded
	case storage.BuildStatusFailed, storage.BuildStatusTimedOut:
		return storage.JobStatusFailed
	case storage.BuildStatusCancelled:
		return storage.JobStatusCancelled
	default:
		return storage.JobStatusRunning
	}
}

func (s *Service) ListJobs(ctx context.Context, ownerID string, canAny bool) ([]storage.Job, error) {
	filter := storage.JobFilter{}
	if !canAny {
		filter.OwnerID = ownerID
	}
	jobs, err := s.jobs.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		jobs[i], _ = s.reconcileJob(ctx, jobs[i])
	}
	return jobs, nil
}

func (s *Service) GetJob(ctx context.Context, id, ownerID string, canAny bool) (storage.Job, error) {
	job, err := s.jobs.Get(ctx, id)
	if err != nil {
		return storage.Job{}, err
	}
	if job.OwnerID != ownerID && !canAny {
		return storage.Job{}, storage.ErrNotFound
	}
	return s.reconcileJob(ctx, job)
}

func (s *Service) CancelJob(ctx context.Context, actor audit.Actor, id, ownerID string, canAny bool) (storage.Job, error) {
	job, err := s.GetJob(ctx, id, ownerID, canAny)
	if err != nil {
		return storage.Job{}, err
	}
	if job.Status.Terminal() {
		return job, nil
	}
	if job.BuildID != "" {
		if _, err := s.builds.Cancel(ctx, actor, job.BuildID); err != nil {
			return storage.Job{}, err
		}
	} else if job.Type == storage.JobTypeWorkspacePrepare && s.engine != nil {
		if err := s.engine.Cancel(ctx, workspacePrepareWorkflowName(job.ID)); err != nil && !errors.Is(err, workflows.ErrNotFound) {
			return storage.Job{}, err
		}
	}
	return s.GetJob(ctx, id, ownerID, canAny)
}

func (s *Service) JobSteps(ctx context.Context, id, ownerID string, canAny bool) ([]storage.JobStep, error) {
	if _, err := s.GetJob(ctx, id, ownerID, canAny); err != nil {
		return nil, err
	}
	return s.jobs.ListSteps(ctx, id)
}

func (s *Service) JobLogs(ctx context.Context, id, ownerID string, canAny bool) (string, error) {
	job, err := s.GetJob(ctx, id, ownerID, canAny)
	if err != nil {
		return "", err
	}
	if job.BuildID != "" {
		return s.builds.Logs(ctx, job.BuildID)
	}
	if job.Type == storage.JobTypeWorkspacePrepare && s.engine != nil {
		logs, err := s.engine.Logs(ctx, workspacePrepareWorkflowName(job.ID))
		if errors.Is(err, workflows.ErrNotFound) {
			return "", nil
		}
		return logs, err
	}
	return "", nil
}

func (s *Service) ReconcileAll(ctx context.Context) error {
	jobs, err := s.jobs.ListReconcilable(ctx)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if _, err := s.reconcileJob(ctx, job); err != nil {
			return fmt.Errorf("devflows: reconciling job %s: %w", job.ID, err)
		}
	}
	return nil
}

func (s *Service) reconcileWorkspace(ctx context.Context, ws storage.Workspace) (storage.Workspace, error) {
	if ws.DeletedAt != nil {
		return ws, nil
	}
	job, err := s.latestWorkspacePrepareJob(ctx, ws.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return ws, nil
	}
	if err != nil {
		return storage.Workspace{}, err
	}
	if _, err := s.reconcileJob(ctx, job); err != nil {
		return storage.Workspace{}, err
	}
	return s.workspaces.Get(ctx, ws.ID)
}

func (s *Service) latestWorkspacePrepareJob(ctx context.Context, workspaceID string) (storage.Job, error) {
	jobs, err := s.jobs.List(ctx, storage.JobFilter{WorkspaceID: workspaceID, Type: storage.JobTypeWorkspacePrepare, Limit: 1})
	if err != nil {
		return storage.Job{}, err
	}
	if len(jobs) == 0 {
		return storage.Job{}, storage.ErrNotFound
	}
	return jobs[0], nil
}

func (s *Service) reconcileJob(ctx context.Context, job storage.Job) (storage.Job, error) {
	if job.Status.Terminal() {
		return job, nil
	}
	switch job.Type {
	case storage.JobTypeBuild:
		return s.reconcileBuildJob(ctx, job)
	case storage.JobTypeWorkspacePrepare:
		return s.reconcileWorkspacePrepareJob(ctx, job)
	default:
		return job, nil
	}
}

func (s *Service) reconcileBuildJob(ctx context.Context, job storage.Job) (storage.Job, error) {
	if job.BuildID == "" {
		return job, nil
	}
	build, err := s.builds.Get(ctx, job.BuildID)
	if err != nil {
		return job, nil
	}
	status := jobStatusFromBuild(build.Status)
	if status != job.Status || build.ReasonCode != job.ReasonCode || build.ErrorMessage != job.ErrorMessage {
		_ = s.jobs.UpdateStatus(ctx, job.ID, status, build.ReasonCode, build.ErrorMessage, build.StartedAt, build.CompletedAt)
		job.Status, job.ReasonCode, job.ErrorMessage, job.StartedAt, job.CompletedAt = status, build.ReasonCode, build.ErrorMessage, build.StartedAt, build.CompletedAt
	}
	return job, nil
}

func (s *Service) reconcileWorkspacePrepareJob(ctx context.Context, job storage.Job) (storage.Job, error) {
	if s.engine == nil {
		return job, nil
	}
	workflowName := workspacePrepareWorkflowName(job.ID)
	status, err := s.engine.Status(ctx, workflowName)
	if errors.Is(err, workflows.ErrNotFound) {
		completedAt := time.Now().UTC()
		job.Status = storage.JobStatusFailed
		job.ReasonCode = "workflow_not_found"
		job.ErrorMessage = "workspace workflow was not found"
		job.CompletedAt = &completedAt
		_ = s.jobs.UpdateStatus(ctx, job.ID, job.Status, job.ReasonCode, job.ErrorMessage, job.StartedAt, job.CompletedAt)
		s.logger.WithContext(ctx).Errorw("workspace provisioning workflow missing",
			"jobID", job.ID,
			"workspaceID", job.WorkspaceID,
			"workflowName", workflowName,
			"reasonCode", job.ReasonCode,
			"errorMessage", job.ErrorMessage,
		)
		_ = s.syncWorkspaceStatusFromJob(ctx, job)
		return job, nil
	}
	if err != nil {
		return job, err
	}

	updated := job
	now := time.Now().UTC()
	switch status.Phase {
	case workflows.PhasePending:
		updated.Status = storage.JobStatusQueued
		updated.ReasonCode = ""
		updated.ErrorMessage = ""
	case workflows.PhaseRunning:
		updated.Status = storage.JobStatusRunning
		updated.ReasonCode = ""
		updated.ErrorMessage = ""
		if updated.StartedAt == nil {
			updated.StartedAt = &now
		}
	case workflows.PhaseSucceeded:
		updated.Status = storage.JobStatusSucceeded
		updated.ReasonCode = ""
		updated.ErrorMessage = ""
		if updated.StartedAt == nil {
			updated.StartedAt = &now
		}
		updated.CompletedAt = &now
	case workflows.PhaseFailed:
		updated.Status = storage.JobStatusFailed
		updated.ReasonCode = "workflow_failed"
		updated.ErrorMessage = strings.TrimSpace(status.Message)
		if updated.ErrorMessage == "" {
			updated.ErrorMessage = "workspace workflow failed"
		}
		updated.CompletedAt = &now
	}
	if updated.Status != job.Status || updated.ReasonCode != job.ReasonCode || updated.ErrorMessage != job.ErrorMessage || !timePtrEqual(updated.StartedAt, job.StartedAt) || !timePtrEqual(updated.CompletedAt, job.CompletedAt) {
		_ = s.jobs.UpdateStatus(ctx, job.ID, updated.Status, updated.ReasonCode, updated.ErrorMessage, updated.StartedAt, updated.CompletedAt)
		s.logger.WithContext(ctx).Infow("workspace provisioning workflow state changed",
			"jobID", job.ID,
			"workspaceID", job.WorkspaceID,
			"workflowName", workflowName,
			"workflowPhase", string(status.Phase),
			"jobStatus", string(updated.Status),
			"reasonCode", updated.ReasonCode,
			"errorMessage", updated.ErrorMessage,
		)
		job = updated
	}
	_ = s.syncWorkspaceStatusFromJob(ctx, job)
	return job, nil
}

func (s *Service) syncWorkspaceStatusFromJob(ctx context.Context, job storage.Job) error {
	if job.WorkspaceID == "" {
		return nil
	}
	ws, err := s.workspaces.Get(ctx, job.WorkspaceID)
	if err != nil {
		return err
	}
	newStatus := ws.Status
	reasonCode := ""
	errorMessage := ""
	switch job.Status {
	case storage.JobStatusQueued, storage.JobStatusRunning:
		newStatus = storage.WorkspaceStatusPending
	case storage.JobStatusSucceeded:
		newStatus = storage.WorkspaceStatusReady
	case storage.JobStatusFailed, storage.JobStatusCancelled:
		newStatus = storage.WorkspaceStatusFailed
		reasonCode = job.ReasonCode
		errorMessage = job.ErrorMessage
	}
	if ws.Status == newStatus && ws.ReasonCode == reasonCode && ws.ErrorMessage == errorMessage {
		return nil
	}
	s.logger.WithContext(ctx).Infow("workspace status reconciled",
		"workspaceID", ws.ID,
		"workspaceName", ws.Name,
		"jobID", job.ID,
		"previousStatus", string(ws.Status),
		"status", string(newStatus),
		"reasonCode", reasonCode,
		"errorMessage", errorMessage,
	)
	return s.workspaces.UpdateStatus(ctx, ws.ID, newStatus, reasonCode, errorMessage)
}

func workspacePrepareWorkflowName(jobID string) string {
	return "workspace-prepare-" + jobID
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
