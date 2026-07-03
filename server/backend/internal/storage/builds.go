package storage

import (
	"context"
	"time"
)

// BuildStatus is the lifecycle state of one build.
type BuildStatus string

const (
	BuildStatusPending   BuildStatus = "pending"
	BuildStatusRunning   BuildStatus = "running"
	BuildStatusSucceeded BuildStatus = "succeeded"
	BuildStatusFailed    BuildStatus = "failed"
	BuildStatusCancelled BuildStatus = "cancelled"
	BuildStatusTimedOut  BuildStatus = "timed_out"
)

// Terminal reports whether status is a final state that reconciliation no
// longer needs to revisit.
func (s BuildStatus) Terminal() bool {
	switch s {
	case BuildStatusSucceeded, BuildStatusFailed, BuildStatusCancelled, BuildStatusTimedOut:
		return true
	default:
		return false
	}
}

// Build is the durable record of one build request: its source
// attribution, target artifact, builder image, and outcome. Argo Workflow
// state is operational, not durable; WorkflowName is only a reference used
// to reconcile status, never authoritative on its own.
type Build struct {
	ID                 string
	OwnerID            string
	Status             BuildStatus
	SourceRepoURL      string
	SourceCommitSHA    string
	ContainerfilePath  string
	ImageRepository    string
	ImageTag           string
	BuilderImageDigest string
	WorkflowName       string
	CancelRequested    bool
	ReasonCode         string
	ErrorMessage       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	StartedAt          *time.Time
	CompletedAt        *time.Time
	DeadlineAt         time.Time
}

// BuildFilter narrows BuildStore.List results.
type BuildFilter struct {
	OwnerID string // empty means every owner
	Limit   int
}

// BuildStore persists Build records.
type BuildStore interface {
	Create(ctx context.Context, b Build) error
	Get(ctx context.Context, id string) (Build, error)
	List(ctx context.Context, filter BuildFilter) ([]Build, error)
	SetWorkflowName(ctx context.Context, id, workflowName string) error
	RequestCancel(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id string, status BuildStatus, reasonCode, errorMessage string, startedAt, completedAt *time.Time) error
	// ListReconcilable returns every non-terminal build, for restart and
	// periodic reconciliation against the workflow engine.
	ListReconcilable(ctx context.Context) ([]Build, error)
}
