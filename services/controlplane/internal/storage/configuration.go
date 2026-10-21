package storage

import (
	"context"
	"time"
)

// ConfigurationRevision is an immutable, validated desired-configuration
// document. It deliberately represents only replayable desired state; a
// PostgreSQL database dump, credentials, sessions, audit events, and runtime
// observations are recovery-set concerns, not configuration revisions.
type ConfigurationRevision struct {
	ID            string
	SchemaVersion string
	Payload       []byte
	PayloadDigest string
	CreatedAt     time.Time
	CreatedBy     string
	Status        string
	ApplyMessage  string
	AppliedAt     *time.Time
}

const (
	ConfigurationRevisionPending = "pending"
	ConfigurationRevisionApplied = "applied"
	ConfigurationRevisionFailed  = "failed"
)

// ConfigurationRevisionStore persists immutable desired-configuration
// revisions and their reconciliation outcome. Implementations must never
// mutate Payload or PayloadDigest after Create.
type ConfigurationRevisionStore interface {
	CreateConfigurationRevision(ctx context.Context, revision ConfigurationRevision) error
	GetConfigurationRevision(ctx context.Context, id string) (ConfigurationRevision, error)
	LatestConfigurationRevision(ctx context.Context) (ConfigurationRevision, error)
	SetConfigurationRevisionStatus(ctx context.Context, id, status, message string, appliedAt *time.Time) error
}
