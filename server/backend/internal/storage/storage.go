// Package storage defines the repository interfaces that sit between
// business logic and persistence. Concrete backends (starting with SQLite in
// internal/storage/sqlite) implement these interfaces; handlers and services
// depend only on this package so a future Postgres-backed implementation can
// be added without touching call sites.
package storage

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by repository lookups that find no matching row.
var ErrNotFound = errors.New("storage: not found")

// ErrConflict is returned when a write would violate a uniqueness or
// optimistic-concurrency constraint.
var ErrConflict = errors.New("storage: conflict")

// DB is the narrow contract every storage backend must satisfy: run
// migrations, execute work in a transaction, produce a consistent backup, and
// report whether it is currently able to serve reads/writes.
type DB interface {
	// Migrate applies any pending schema migrations. It must refuse to start
	// against an unknown or partially applied schema version rather than
	// guessing a fallback.
	Migrate(ctx context.Context) error

	// WithTx runs fn inside a single database transaction, committing on nil
	// error and rolling back otherwise.
	WithTx(ctx context.Context, fn func(ctx context.Context) error) error

	// Ping reports whether the database is reachable and writable, for use by
	// the readiness endpoint.
	Ping(ctx context.Context) error

	// Backup writes a transactionally consistent snapshot of the database to
	// destPath.
	Backup(ctx context.Context, destPath string) error

	// Close releases all database resources.
	Close() error
}

// OperationKind identifies the kind of long-running async operation tracked
// by OperationsStore, per the plan's durable asynchronous operation model.
type OperationKind string

// OperationStatus is the lifecycle state of a durable operation.
type OperationStatus string

const (
	OperationStatusPending   OperationStatus = "pending"
	OperationStatusRunning   OperationStatus = "running"
	OperationStatusSucceeded OperationStatus = "succeeded"
	OperationStatusFailed    OperationStatus = "failed"
)

// Operation is a durable, owner/RBAC-filtered record of a non-immediate
// action such as artifact deletion or audit export.
type Operation struct {
	ID          string
	Kind        OperationKind
	OwnerID     string
	Status      OperationStatus
	ResultBody  []byte
	ProblemBody []byte
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// OperationsStore persists Operation records.
type OperationsStore interface {
	Create(ctx context.Context, op Operation) error
	Get(ctx context.Context, id string) (Operation, error)
	UpdateStatus(ctx context.Context, id string, status OperationStatus, resultBody, problemBody []byte) error
}

// IdempotencyRecord is a cached response for a previously seen idempotency
// key, retained for the plan's accepted 24-hour idempotency window.
type IdempotencyRecord struct {
	Key            string
	Scope          string
	RequestHash    string
	ResponseStatus int
	ResponseBody   []byte
	CreatedAt      time.Time
	ExpiresAt      time.Time
}

// IdempotencyStore persists IdempotencyRecord entries keyed by (scope, key).
type IdempotencyStore interface {
	// Reserve attempts to atomically claim key within scope. It returns the
	// existing record and false if the key is already reserved (whether or
	// not it has completed), or a zero record and true if this call claimed
	// it.
	Reserve(ctx context.Context, scope, key, requestHash string, ttl time.Duration) (existing IdempotencyRecord, claimed bool, err error)

	// Complete stores the final response for a previously reserved key.
	Complete(ctx context.Context, scope, key string, status int, body []byte) error
}

// MaintenanceCheckpoint tracks the cursor and last-run time for one named
// in-process maintenance task, so restarts resume rather than restart from
// scratch or duplicate work.
type MaintenanceCheckpoint struct {
	TaskName  string
	LastRunAt time.Time
	Cursor    string
	UpdatedAt time.Time
}

// MaintenanceStore persists MaintenanceCheckpoint entries.
type MaintenanceStore interface {
	Get(ctx context.Context, taskName string) (MaintenanceCheckpoint, error)
	Save(ctx context.Context, checkpoint MaintenanceCheckpoint) error
}
