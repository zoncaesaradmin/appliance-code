package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/server/backend/internal/storage"
)

// BuildStore is the SQLite-backed storage.BuildStore.
type BuildStore struct {
	db *DB
}

// NewBuildStore returns a BuildStore backed by db.
func NewBuildStore(db *DB) *BuildStore {
	return &BuildStore{db: db}
}

func (s *BuildStore) Create(ctx context.Context, b storage.Build) error {
	now := time.Now().UTC()
	if b.CreatedAt.IsZero() {
		b.CreatedAt = now
	}
	if b.UpdatedAt.IsZero() {
		b.UpdatedAt = now
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO builds (
			id, owner_id, status, source_repo_url, source_commit_sha, containerfile_path,
			image_repository, image_tag, builder_image_digest, workflow_name, cancel_requested,
			reason_code, error_message, created_at, updated_at, started_at, completed_at, deadline_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.OwnerID, string(b.Status), b.SourceRepoURL, b.SourceCommitSHA, b.ContainerfilePath,
		b.ImageRepository, b.ImageTag, b.BuilderImageDigest, nullableString(b.WorkflowName), boolToInt(b.CancelRequested),
		nullableString(b.ReasonCode), nullableString(b.ErrorMessage),
		b.CreatedAt.Format(time.RFC3339Nano), b.UpdatedAt.Format(time.RFC3339Nano),
		formatTimePtr(b.StartedAt), formatTimePtr(b.CompletedAt), b.DeadlineAt.Format(time.RFC3339Nano),
	)
	if isUniqueConstraintErr(err) {
		return storage.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating build %s: %w", b.ID, err)
	}
	return nil
}

func formatTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

const selectBuildColumns = `
	id, owner_id, status, source_repo_url, source_commit_sha, containerfile_path,
	image_repository, image_tag, builder_image_digest, workflow_name, cancel_requested,
	reason_code, error_message, created_at, updated_at, started_at, completed_at, deadline_at`

func scanBuild(row interface{ Scan(dest ...any) error }) (storage.Build, error) {
	var (
		b                                      storage.Build
		status                                 string
		workflowName, reasonCode, errorMessage sql.NullString
		cancelRequested                        int
		createdAt, updatedAt, deadlineAt       string
		startedAt, completedAt                 sql.NullString
	)
	if err := row.Scan(
		&b.ID, &b.OwnerID, &status, &b.SourceRepoURL, &b.SourceCommitSHA, &b.ContainerfilePath,
		&b.ImageRepository, &b.ImageTag, &b.BuilderImageDigest, &workflowName, &cancelRequested,
		&reasonCode, &errorMessage, &createdAt, &updatedAt, &startedAt, &completedAt, &deadlineAt,
	); err != nil {
		return storage.Build{}, err
	}

	b.Status = storage.BuildStatus(status)
	b.WorkflowName = workflowName.String
	b.CancelRequested = cancelRequested != 0
	b.ReasonCode = reasonCode.String
	b.ErrorMessage = errorMessage.String

	var err error
	if b.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.Build{}, err
	}
	if b.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return storage.Build{}, err
	}
	if b.DeadlineAt, err = time.Parse(time.RFC3339Nano, deadlineAt); err != nil {
		return storage.Build{}, err
	}
	if startedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, startedAt.String)
		if err != nil {
			return storage.Build{}, err
		}
		b.StartedAt = &t
	}
	if completedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return storage.Build{}, err
		}
		b.CompletedAt = &t
	}
	return b, nil
}

func (s *BuildStore) Get(ctx context.Context, id string) (storage.Build, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectBuildColumns+` FROM builds WHERE id = ?`, id)
	b, err := scanBuild(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Build{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Build{}, fmt.Errorf("sqlite: getting build %s: %w", id, err)
	}
	return b, nil
}

func (s *BuildStore) List(ctx context.Context, filter storage.BuildFilter) ([]storage.Build, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + selectBuildColumns + ` FROM builds WHERE 1 = 1`
	var args []any
	if filter.OwnerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, filter.OwnerID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing builds: %w", err)
	}
	defer rows.Close()

	var builds []storage.Build
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (s *BuildStore) SetWorkflowName(ctx context.Context, id, workflowName string) error {
	return s.exec1(ctx, `UPDATE builds SET workflow_name = ?, updated_at = ? WHERE id = ?`,
		workflowName, time.Now().UTC().Format(time.RFC3339Nano), id)
}

func (s *BuildStore) RequestCancel(ctx context.Context, id string) error {
	return s.exec1(ctx, `UPDATE builds SET cancel_requested = 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
}

func (s *BuildStore) UpdateStatus(ctx context.Context, id string, status storage.BuildStatus, reasonCode, errorMessage string, startedAt, completedAt *time.Time) error {
	return s.exec1(ctx, `
		UPDATE builds SET status = ?, reason_code = ?, error_message = ?, started_at = COALESCE(?, started_at),
			completed_at = COALESCE(?, completed_at), updated_at = ?
		WHERE id = ?`,
		string(status), nullableString(reasonCode), nullableString(errorMessage),
		formatTimePtr(startedAt), formatTimePtr(completedAt), time.Now().UTC().Format(time.RFC3339Nano), id,
	)
}

func (s *BuildStore) ListReconcilable(ctx context.Context) ([]storage.Build, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `
		SELECT `+selectBuildColumns+` FROM builds
		WHERE status IN (?, ?) ORDER BY created_at ASC`,
		string(storage.BuildStatusPending), string(storage.BuildStatusRunning),
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing reconcilable builds: %w", err)
	}
	defer rows.Close()

	var builds []storage.Build
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (s *BuildStore) exec1(ctx context.Context, query string, args ...any) error {
	res, err := s.db.q(ctx).ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("sqlite: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: checking update result: %w", err)
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}
