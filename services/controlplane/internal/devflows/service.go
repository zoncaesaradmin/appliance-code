package devflows

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/builds"
	"appliance-code/services/controlplane/internal/storage"
)

var (
	ErrNoCurrentWorkspace     = errors.New("devflows: no current workspace selected")
	ErrForbidden              = errors.New("devflows: forbidden")
	ErrWorkspaceHasActiveJobs = errors.New("devflows: workspace has active jobs")
)

type Service struct {
	catalog    Catalog
	workspaces storage.WorkspaceStore
	jobs       storage.JobStore
	builds     *builds.Service
	audit      *audit.Recorder
}

func NewService(catalog Catalog, workspaces storage.WorkspaceStore, jobs storage.JobStore, buildsSvc *builds.Service, recorder *audit.Recorder) *Service {
	return &Service{catalog: catalog, workspaces: workspaces, jobs: jobs, builds: buildsSvc, audit: recorder}
}

func (s *Service) Catalog() Catalog { return s.catalog }

type CreateWorkspaceRequest struct {
	Name                string
	WorkProfile         string
	Repo                string
	SourceRef           string
	SourceCredentialRef string
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
	repo, ok := s.catalog.Repo(req.Repo)
	if !ok {
		return storage.Workspace{}, fmt.Errorf("devflows: unknown repo %q", req.Repo)
	}
	if req.SourceCredentialRef != "" && req.SourceCredentialRef != repo.SourceCredentialRef {
		return storage.Workspace{}, fmt.Errorf("devflows: source credential override is not allowed for repo %q", repo.Name)
	}
	ref := strings.TrimSpace(req.SourceRef)
	if ref == "" {
		ref = repo.DefaultRef
	}
	if ref == "" {
		return storage.Workspace{}, fmt.Errorf("devflows: sourceRef is required")
	}
	now := time.Now().UTC()
	ws := storage.Workspace{ID: uuid.Must(uuid.NewV7()).String(), OwnerID: ownerID, Name: name, WorkProfile: profile,
		SourceRepoURL: repo.URL, SourceRef: ref, SourceCredentialRef: repo.SourceCredentialRef, Status: storage.WorkspaceStatusReady,
		CreatedAt: now, UpdatedAt: now}
	if err := s.workspaces.Create(ctx, ws); err != nil {
		return storage.Workspace{}, err
	}
	_ = s.workspaces.SetCurrent(ctx, ownerID, ws.ID)
	if s.audit != nil {
		_ = s.audit.Record(ctx, actor, audit.Event{Action: "workspaces.create", TargetType: "workspace", TargetID: ws.ID, Outcome: storage.AuditOutcomeSuccess})
	}
	return ws, nil
}

func (s *Service) ListWorkspaces(ctx context.Context, ownerID string, canAny bool) ([]storage.Workspace, error) {
	filter := storage.WorkspaceFilter{}
	if !canAny {
		filter.OwnerID = ownerID
	}
	return s.workspaces.List(ctx, filter)
}

func (s *Service) GetWorkspace(ctx context.Context, id, ownerID string, canAny bool) (storage.Workspace, error) {
	ws, err := s.workspaces.Get(ctx, id)
	if err != nil {
		return storage.Workspace{}, err
	}
	if ws.OwnerID != ownerID && !canAny {
		return storage.Workspace{}, storage.ErrNotFound
	}
	return ws, nil
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
	return ws, nil
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
	return ws, nil
}

func (s *Service) ListBuildTargetsForCurrent(ctx context.Context, userID string) ([]BuildTarget, error) {
	ws, err := s.CurrentWorkspace(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.catalog.TargetsForProfile(ws.WorkProfile), nil
}

func (s *Service) SubmitBuildForCurrent(ctx context.Context, actor audit.Actor, ownerID string, req SubmitBuildRequest, idempotencyKey string) (storage.Job, error) {
	ws, err := s.CurrentWorkspace(ctx, ownerID)
	if err != nil {
		return storage.Job{}, err
	}
	resolved, err := s.catalog.ResolveTarget(ws.WorkProfile, req.TargetName)
	if err != nil {
		return storage.Job{}, err
	}
	if normalizeName(resolved.Repo.Name) != normalizeName(s.repoNameForURL(ws.SourceRepoURL)) && resolved.Repo.URL != ws.SourceRepoURL {
		return storage.Job{}, fmt.Errorf("devflows: build target %q does not match current workspace repo", req.TargetName)
	}
	if !IsCommitSHA(ws.SourceRef) {
		return storage.Job{}, fmt.Errorf("devflows: sourceRef %q is mutable; commit SHA resolution is required before build", ws.SourceRef)
	}
	tag := strings.TrimSpace(req.ImageTag)
	if tag == "" {
		tag = renderTag(resolved.Target.ImageTagTemplate, ws, resolved.Target)
	}
	if tag == "" {
		tag = ws.SourceRef[:12]
	}
	buildReq := builds.CreateRequest{SourceRepoURL: ws.SourceRepoURL, SourceCommitSHA: ws.SourceRef,
		Execution: resolved.Target.Execution, ScriptPath: resolved.Target.ScriptPath, MakeTarget: resolved.Target.MakeTarget,
		ContainerfilePath: resolved.Target.ContainerfilePath, ImageRepository: resolved.Target.ImageRepository, ImageTag: tag,
		BuilderImageDigest: resolved.Target.BuilderImageDigest, SourceCredentialRef: ws.SourceCredentialRef}
	if ws.SourceCredentialRef != "" {
		cred, ok := s.catalog.SourceCredential(ws.SourceCredentialRef)
		if !ok {
			return storage.Job{}, fmt.Errorf("devflows: source credential %q is not configured", ws.SourceCredentialRef)
		}
		buildReq.SourceCredentialSecret = cred.KubernetesSecretName
		buildReq.KnownHostsSecret = cred.KnownHostsSecretName
	}
	build, err := s.builds.Create(ctx, actor, ownerID, buildReq, idempotencyKey)
	if err != nil {
		return storage.Job{}, err
	}
	existingJobs, err := s.jobs.List(ctx, storage.JobFilter{OwnerID: ownerID, WorkspaceID: ws.ID, Limit: 200})
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
	jobs, err := s.jobs.List(ctx, storage.JobFilter{OwnerID: userID, WorkspaceID: ws.ID, Limit: 1})
	if err != nil {
		return storage.Job{}, err
	}
	if len(jobs) == 0 {
		return storage.Job{}, storage.ErrNotFound
	}
	return s.reconcileJob(ctx, jobs[0])
}

func (s *Service) repoNameForURL(url string) string {
	for _, repo := range s.catalog.Repos {
		if repo.URL == url {
			return repo.Name
		}
	}
	return ""
}

func renderTag(tmpl string, ws storage.Workspace, target BuildTarget) string {
	if strings.TrimSpace(tmpl) == "" {
		return ""
	}
	return strings.NewReplacer("{workspace}", ws.Name, "{target}", target.Name, "{commit12}", ws.SourceRef[:min(12, len(ws.SourceRef))]).Replace(tmpl)
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
	if job.BuildID != "" && !job.Status.Terminal() {
		if _, err := s.builds.Cancel(ctx, actor, job.BuildID); err != nil {
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
	if job.BuildID == "" {
		return "", nil
	}
	return s.builds.Logs(ctx, job.BuildID)
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

func (s *Service) reconcileJob(ctx context.Context, job storage.Job) (storage.Job, error) {
	if job.BuildID == "" || job.Status.Terminal() {
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
