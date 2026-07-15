package storage

import (
	"context"
	"time"
)

type WorkspaceStatus string

const (
	WorkspaceStatusPending WorkspaceStatus = "pending"
	WorkspaceStatusReady   WorkspaceStatus = "ready"
	WorkspaceStatusFailed  WorkspaceStatus = "failed"
	WorkspaceStatusDeleted WorkspaceStatus = "deleted"
)

type Workspace struct {
	ID                  string
	OwnerID             string
	Name                string
	WorkProfile         string
	SourceRepoURL       string
	SourceRef           string
	SourceCredentialRef string
	Status              WorkspaceStatus
	ReasonCode          string
	ErrorMessage        string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

type WorkspaceFilter struct {
	OwnerID        string
	IncludeDeleted bool
	Limit          int
}

type CurrentWorkspace struct {
	UserID      string
	WorkspaceID string
	UpdatedAt   time.Time
}

type WorkspaceStore interface {
	Create(ctx context.Context, ws Workspace) error
	Get(ctx context.Context, id string) (Workspace, error)
	List(ctx context.Context, filter WorkspaceFilter) ([]Workspace, error)
	MarkDeleted(ctx context.Context, id string, deletedAt time.Time) error
	SetCurrent(ctx context.Context, userID, workspaceID string) error
	GetCurrent(ctx context.Context, userID string) (CurrentWorkspace, error)
}

type JobType string

const (
	JobTypeWorkspacePrepare JobType = "workspace_prepare"
	JobTypeBuild            JobType = "build"
	JobTypeDeploy           JobType = "deploy"
)

type JobStatus string

const (
	JobStatusQueued    JobStatus = "queued"
	JobStatusRunning   JobStatus = "running"
	JobStatusSucceeded JobStatus = "succeeded"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

func (s JobStatus) Terminal() bool {
	switch s {
	case JobStatusSucceeded, JobStatusFailed, JobStatusCancelled:
		return true
	default:
		return false
	}
}

type Job struct {
	ID           string
	OwnerID      string
	WorkspaceID  string
	BuildID      string
	Type         JobType
	Status       JobStatus
	TargetName   string
	ArtifactRef  string
	ReasonCode   string
	ErrorMessage string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

type JobFilter struct {
	OwnerID     string
	WorkspaceID string
	Limit       int
}

type JobStep struct {
	ID          string
	JobID       string
	Name        string
	Status      JobStatus
	Message     string
	CreatedAt   time.Time
	StartedAt   *time.Time
	CompletedAt *time.Time
}

type JobStore interface {
	Create(ctx context.Context, job Job) error
	Get(ctx context.Context, id string) (Job, error)
	List(ctx context.Context, filter JobFilter) ([]Job, error)
	ListReconcilable(ctx context.Context) ([]Job, error)
	UpdateStatus(ctx context.Context, id string, status JobStatus, reasonCode, errorMessage string, startedAt, completedAt *time.Time) error
	AddStep(ctx context.Context, step JobStep) error
	ListSteps(ctx context.Context, jobID string) ([]JobStep, error)
}
