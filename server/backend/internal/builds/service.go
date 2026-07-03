package builds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"appliance-code/server/backend/internal/audit"
	"appliance-code/server/backend/internal/registryauth"
	"appliance-code/server/backend/internal/storage"
	"appliance-code/server/backend/internal/workflows"
)

// idempotencyScope namespaces build-creation idempotency keys within the
// shared storage.IdempotencyStore.
const idempotencyScope = "builds.create"

// idempotencyTTL is how long a build-creation idempotency key is honored,
// matching the plan's accepted 24-hour idempotency window.
const idempotencyTTL = 24 * time.Hour

// ErrIdempotencyKeyReused is returned when a client reuses an idempotency
// key with a materially different request body.
var ErrIdempotencyKeyReused = errors.New("builds: idempotency key already used with a different request")

// ErrIdempotencyInProgress is returned when a concurrent request with the
// same idempotency key has not finished yet.
var ErrIdempotencyInProgress = errors.New("builds: a request with this idempotency key is still being processed")

// CreateRequest describes a new build. ContainerfilePath defaults to
// "Containerfile" (a literal "Dockerfile" is accepted only as a
// Buildah-compatible filename alias, per the plan).
type CreateRequest struct {
	SourceRepoURL      string
	SourceCommitSHA    string
	ContainerfilePath  string
	ImageRepository    string
	ImageTag           string
	BuilderImageDigest string
}

// Service implements build request business logic above storage.BuildStore
// and workflows.Engine.
type Service struct {
	db                   storage.DB
	builds               storage.BuildStore
	idempotency          storage.IdempotencyStore
	engine               workflows.Engine
	audit                *audit.Recorder
	allowedGitHosts      []string
	allowedBuilderImages []string
	defaultDeadline      time.Duration
}

// NewService wires a Service from its storage, workflow-engine, and policy
// dependencies.
func NewService(
	db storage.DB, buildStore storage.BuildStore, idempotency storage.IdempotencyStore, engine workflows.Engine,
	recorder *audit.Recorder, allowedGitHosts, allowedBuilderImages []string, defaultDeadline time.Duration,
) *Service {
	return &Service{
		db: db, builds: buildStore, idempotency: idempotency, engine: engine, audit: recorder,
		allowedGitHosts: allowedGitHosts, allowedBuilderImages: allowedBuilderImages, defaultDeadline: defaultDeadline,
	}
}

func hashRequest(req CreateRequest) string {
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Create validates req, then submits it as a new build. If idempotencyKey
// is non-empty and was already used with an identical request, the
// previously created build is returned instead of creating a duplicate.
func (s *Service) Create(ctx context.Context, actor audit.Actor, ownerID string, req CreateRequest, idempotencyKey string) (storage.Build, error) {
	if err := ValidateSource(req.SourceRepoURL, req.SourceCommitSHA, s.allowedGitHosts); err != nil {
		return storage.Build{}, err
	}
	if err := ValidateBuilderImage(req.BuilderImageDigest, s.allowedBuilderImages); err != nil {
		return storage.Build{}, err
	}
	imageRepo, err := registryauth.NormalizeRepositoryName(req.ImageRepository)
	if err != nil {
		return storage.Build{}, err
	}
	if req.ImageTag == "" {
		return storage.Build{}, fmt.Errorf("builds: imageTag is required")
	}
	containerfilePath := req.ContainerfilePath
	if containerfilePath == "" {
		containerfilePath = "Containerfile"
	}

	if idempotencyKey != "" {
		requestHash := hashRequest(req)
		existing, claimed, err := s.idempotency.Reserve(ctx, idempotencyScope, idempotencyKey, requestHash, idempotencyTTL)
		if err != nil {
			return storage.Build{}, err
		}
		if !claimed {
			if existing.RequestHash != requestHash {
				return storage.Build{}, ErrIdempotencyKeyReused
			}
			if len(existing.ResponseBody) == 0 {
				return storage.Build{}, ErrIdempotencyInProgress
			}
			return s.builds.Get(ctx, string(existing.ResponseBody))
		}
	}

	now := time.Now().UTC()
	build := storage.Build{
		ID: uuid.Must(uuid.NewV7()).String(), OwnerID: ownerID, Status: storage.BuildStatusPending,
		SourceRepoURL: req.SourceRepoURL, SourceCommitSHA: req.SourceCommitSHA, ContainerfilePath: containerfilePath,
		ImageRepository: imageRepo, ImageTag: req.ImageTag, BuilderImageDigest: req.BuilderImageDigest,
		CreatedAt: now, UpdatedAt: now, DeadlineAt: now.Add(s.defaultDeadline),
	}

	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.builds.Create(ctx, build); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "builds.create", TargetType: "build", TargetID: build.ID, Outcome: storage.AuditOutcomeSuccess,
			Details: map[string]any{"sourceRepoURL": req.SourceRepoURL, "sourceCommitSHA": req.SourceCommitSHA, "imageRepository": imageRepo},
		})
	})
	if err != nil {
		return storage.Build{}, err
	}

	workflowName := "build-" + build.ID
	submitErr := s.engine.Submit(ctx, workflows.Spec{
		Name: workflowName, SourceRepoURL: build.SourceRepoURL, SourceCommitSHA: build.SourceCommitSHA,
		ContainerfilePath: build.ContainerfilePath, BuilderImageDigest: build.BuilderImageDigest,
		TargetRepository: build.ImageRepository, TargetTag: build.ImageTag, Deadline: build.DeadlineAt,
	})
	completedAt := time.Now().UTC()
	if submitErr != nil {
		_ = s.builds.UpdateStatus(ctx, build.ID, storage.BuildStatusFailed, "workflow_submit_failed", submitErr.Error(), nil, &completedAt)
		build.Status = storage.BuildStatusFailed
		build.ReasonCode = "workflow_submit_failed"
		build.ErrorMessage = submitErr.Error()
		build.CompletedAt = &completedAt
	} else {
		_ = s.builds.SetWorkflowName(ctx, build.ID, workflowName)
		_ = s.builds.UpdateStatus(ctx, build.ID, storage.BuildStatusRunning, "", "", &completedAt, nil)
		build.WorkflowName = workflowName
		build.Status = storage.BuildStatusRunning
		build.StartedAt = &completedAt
	}

	if idempotencyKey != "" {
		_ = s.idempotency.Complete(ctx, idempotencyScope, idempotencyKey, 201, []byte(build.ID))
	}

	return build, nil
}

// Get returns a build after reconciling its status against the workflow
// engine.
func (s *Service) Get(ctx context.Context, id string) (storage.Build, error) {
	b, err := s.builds.Get(ctx, id)
	if err != nil {
		return storage.Build{}, err
	}
	return s.reconcile(ctx, b)
}

// List returns builds matching filter, reconciling each non-terminal one.
func (s *Service) List(ctx context.Context, filter storage.BuildFilter) ([]storage.Build, error) {
	list, err := s.builds.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	out := make([]storage.Build, len(list))
	for i, b := range list {
		out[i], err = s.reconcile(ctx, b)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Cancel requests cancellation of a build and reconciles its resulting
// status.
func (s *Service) Cancel(ctx context.Context, actor audit.Actor, id string) (storage.Build, error) {
	b, err := s.builds.Get(ctx, id)
	if err != nil {
		return storage.Build{}, err
	}
	if b.Status.Terminal() {
		return b, nil
	}
	if err := s.builds.RequestCancel(ctx, id); err != nil {
		return storage.Build{}, err
	}
	b.CancelRequested = true

	if err := s.audit.Record(ctx, actor, audit.Event{
		Action: "builds.cancel", TargetType: "build", TargetID: id, Outcome: storage.AuditOutcomeSuccess,
	}); err != nil {
		return storage.Build{}, err
	}

	return s.reconcile(ctx, b)
}

// Logs returns a build's available log output via the workflow engine.
func (s *Service) Logs(ctx context.Context, id string) (string, error) {
	b, err := s.builds.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if b.WorkflowName == "" {
		return "", nil
	}
	logs, err := s.engine.Logs(ctx, b.WorkflowName)
	if errors.Is(err, workflows.ErrNotFound) {
		return "", nil
	}
	return logs, err
}

// ReconcileAll refreshes every non-terminal build's status against the
// workflow engine, for restart recovery and periodic maintenance polling.
func (s *Service) ReconcileAll(ctx context.Context) error {
	pending, err := s.builds.ListReconcilable(ctx)
	if err != nil {
		return err
	}
	for _, b := range pending {
		if _, err := s.reconcile(ctx, b); err != nil {
			return fmt.Errorf("builds: reconciling build %s: %w", b.ID, err)
		}
	}
	return nil
}

func (s *Service) reconcile(ctx context.Context, b storage.Build) (storage.Build, error) {
	if b.Status.Terminal() {
		return b, nil
	}

	now := time.Now().UTC()
	if now.After(b.DeadlineAt) {
		if b.WorkflowName != "" {
			_ = s.engine.Cancel(ctx, b.WorkflowName)
		}
		if err := s.builds.UpdateStatus(ctx, b.ID, storage.BuildStatusTimedOut, "deadline_exceeded", "build exceeded its deadline", nil, &now); err != nil {
			return storage.Build{}, err
		}
		b.Status, b.ReasonCode, b.ErrorMessage, b.CompletedAt = storage.BuildStatusTimedOut, "deadline_exceeded", "build exceeded its deadline", &now
		return b, nil
	}

	if b.CancelRequested && b.WorkflowName != "" {
		_ = s.engine.Cancel(ctx, b.WorkflowName)
	}

	if b.WorkflowName == "" {
		return b, nil
	}

	status, err := s.engine.Status(ctx, b.WorkflowName)
	if errors.Is(err, workflows.ErrNotFound) {
		return b, nil
	}
	if err != nil {
		return storage.Build{}, fmt.Errorf("builds: getting workflow status for build %s: %w", b.ID, err)
	}

	switch status.Phase {
	case workflows.PhaseSucceeded:
		if err := s.builds.UpdateStatus(ctx, b.ID, storage.BuildStatusSucceeded, "", "", nil, &now); err != nil {
			return storage.Build{}, err
		}
		b.Status, b.CompletedAt = storage.BuildStatusSucceeded, &now
	case workflows.PhaseFailed:
		final, reason := storage.BuildStatusFailed, "build_failed"
		if b.CancelRequested {
			final, reason = storage.BuildStatusCancelled, "cancelled"
		}
		if err := s.builds.UpdateStatus(ctx, b.ID, final, reason, status.Message, nil, &now); err != nil {
			return storage.Build{}, err
		}
		b.Status, b.ReasonCode, b.ErrorMessage, b.CompletedAt = final, reason, status.Message, &now
	case workflows.PhaseRunning:
		if b.Status != storage.BuildStatusRunning {
			if err := s.builds.UpdateStatus(ctx, b.ID, storage.BuildStatusRunning, "", "", &now, nil); err != nil {
				return storage.Build{}, err
			}
			b.Status = storage.BuildStatusRunning
		}
	}
	return b, nil
}
