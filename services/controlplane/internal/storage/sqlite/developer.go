package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

type WorkspaceStore struct{ db *DB }

func NewWorkspaceStore(db *DB) *WorkspaceStore { return &WorkspaceStore{db: db} }

func (s *WorkspaceStore) Create(ctx context.Context, ws storage.Workspace) error {
	now := time.Now().UTC()
	if ws.CreatedAt.IsZero() {
		ws.CreatedAt = now
	}
	if ws.UpdatedAt.IsZero() {
		ws.UpdatedAt = now
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO workspaces (
			id, owner_id, name, work_profile, source_repo_url, source_ref, source_credential_ref,
			status, reason_code, error_message, created_at, updated_at, deleted_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ws.ID, ws.OwnerID, ws.Name, ws.WorkProfile, ws.SourceRepoURL, ws.SourceRef, nullableString(ws.SourceCredentialRef),
		string(ws.Status), nullableString(ws.ReasonCode), nullableString(ws.ErrorMessage),
		ws.CreatedAt.Format(time.RFC3339Nano), ws.UpdatedAt.Format(time.RFC3339Nano), formatTimePtr(ws.DeletedAt),
	)
	if isUniqueConstraintErr(err) {
		return storage.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating workspace %s: %w", ws.ID, err)
	}
	return nil
}

const selectWorkspaceColumns = `
	id, owner_id, name, work_profile, source_repo_url, source_ref, source_credential_ref,
	status, reason_code, error_message, created_at, updated_at, deleted_at`

func scanWorkspace(row interface{ Scan(dest ...any) error }) (storage.Workspace, error) {
	var ws storage.Workspace
	var status, createdAt, updatedAt string
	var cred, reason, msg, deletedAt sql.NullString
	if err := row.Scan(&ws.ID, &ws.OwnerID, &ws.Name, &ws.WorkProfile, &ws.SourceRepoURL, &ws.SourceRef, &cred,
		&status, &reason, &msg, &createdAt, &updatedAt, &deletedAt); err != nil {
		return storage.Workspace{}, err
	}
	ws.SourceCredentialRef = cred.String
	ws.Status = storage.WorkspaceStatus(status)
	ws.ReasonCode = reason.String
	ws.ErrorMessage = msg.String
	var err error
	if ws.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.Workspace{}, err
	}
	if ws.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return storage.Workspace{}, err
	}
	if deletedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, deletedAt.String)
		if err != nil {
			return storage.Workspace{}, err
		}
		ws.DeletedAt = &t
	}
	return ws, nil
}

func (s *WorkspaceStore) Get(ctx context.Context, id string) (storage.Workspace, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectWorkspaceColumns+` FROM workspaces WHERE id = ?`, id)
	ws, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Workspace{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Workspace{}, fmt.Errorf("sqlite: getting workspace %s: %w", id, err)
	}
	return ws, nil
}

func (s *WorkspaceStore) List(ctx context.Context, filter storage.WorkspaceFilter) ([]storage.Workspace, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + selectWorkspaceColumns + ` FROM workspaces WHERE 1 = 1`
	var args []any
	if filter.OwnerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, filter.OwnerID)
	}
	if !filter.IncludeDeleted {
		query += ` AND deleted_at IS NULL`
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing workspaces: %w", err)
	}
	defer rows.Close()
	var out []storage.Workspace
	for rows.Next() {
		ws, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

func (s *WorkspaceStore) UpdateStatus(ctx context.Context, id string, status storage.WorkspaceStatus, reasonCode, errorMessage string) error {
	return execOne(ctx, s.db, `
		UPDATE workspaces SET status = ?, reason_code = ?, error_message = ?, updated_at = ?
		WHERE id = ?`, string(status), nullableString(reasonCode), nullableString(errorMessage), time.Now().UTC().Format(time.RFC3339Nano), id)
}

func (s *WorkspaceStore) MarkDeleted(ctx context.Context, id string, deletedAt time.Time) error {
	return execOne(ctx, s.db, `UPDATE workspaces SET status = ?, deleted_at = ?, updated_at = ? WHERE id = ?`,
		string(storage.WorkspaceStatusDeleted), deletedAt.UTC().Format(time.RFC3339Nano), deletedAt.UTC().Format(time.RFC3339Nano), id)
}

func (s *WorkspaceStore) SetCurrent(ctx context.Context, userID, workspaceID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO current_workspaces (user_id, workspace_id, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET workspace_id = excluded.workspace_id, updated_at = excluded.updated_at`, userID, workspaceID, now)
	if err != nil {
		return fmt.Errorf("sqlite: setting current workspace: %w", err)
	}
	return nil
}

func (s *WorkspaceStore) GetCurrent(ctx context.Context, userID string) (storage.CurrentWorkspace, error) {
	var cur storage.CurrentWorkspace
	var updatedAt string
	err := s.db.q(ctx).QueryRowContext(ctx, `SELECT user_id, workspace_id, updated_at FROM current_workspaces WHERE user_id = ?`, userID).Scan(&cur.UserID, &cur.WorkspaceID, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.CurrentWorkspace{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.CurrentWorkspace{}, fmt.Errorf("sqlite: getting current workspace: %w", err)
	}
	var parseErr error
	cur.UpdatedAt, parseErr = time.Parse(time.RFC3339Nano, updatedAt)
	if parseErr != nil {
		return storage.CurrentWorkspace{}, parseErr
	}
	return cur, nil
}

type JobStore struct{ db *DB }

func NewJobStore(db *DB) *JobStore { return &JobStore{db: db} }

func (s *JobStore) Create(ctx context.Context, job storage.Job) error {
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO jobs (
			id, owner_id, workspace_id, build_id, type, status, target_name, artifact_ref, reason_code, error_message,
			created_at, updated_at, started_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID, job.OwnerID, nullableString(job.WorkspaceID), nullableString(job.BuildID), string(job.Type), string(job.Status), nullableString(job.TargetName), nullableString(job.ArtifactRef),
		nullableString(job.ReasonCode), nullableString(job.ErrorMessage), job.CreatedAt.Format(time.RFC3339Nano), job.UpdatedAt.Format(time.RFC3339Nano),
		formatTimePtr(job.StartedAt), formatTimePtr(job.CompletedAt),
	)
	if isUniqueConstraintErr(err) {
		return storage.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating job %s: %w", job.ID, err)
	}
	return nil
}

const selectJobColumns = `
	id, owner_id, workspace_id, build_id, type, status, target_name, artifact_ref, reason_code, error_message,
	created_at, updated_at, started_at, completed_at`

func scanJob(row interface{ Scan(dest ...any) error }) (storage.Job, error) {
	var job storage.Job
	var typ, status, createdAt, updatedAt string
	var workspaceID, buildID, target, artifactRef, reason, msg, startedAt, completedAt sql.NullString
	if err := row.Scan(&job.ID, &job.OwnerID, &workspaceID, &buildID, &typ, &status, &target, &artifactRef, &reason, &msg,
		&createdAt, &updatedAt, &startedAt, &completedAt); err != nil {
		return storage.Job{}, err
	}
	job.WorkspaceID = workspaceID.String
	job.BuildID = buildID.String
	job.Type = storage.JobType(typ)
	job.Status = storage.JobStatus(status)
	job.TargetName = target.String
	job.ArtifactRef = artifactRef.String
	job.ReasonCode = reason.String
	job.ErrorMessage = msg.String
	var err error
	if job.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.Job{}, err
	}
	if job.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return storage.Job{}, err
	}
	if startedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, startedAt.String)
		if err != nil {
			return storage.Job{}, err
		}
		job.StartedAt = &t
	}
	if completedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return storage.Job{}, err
		}
		job.CompletedAt = &t
	}
	return job, nil
}

func (s *JobStore) Get(ctx context.Context, id string) (storage.Job, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectJobColumns+` FROM jobs WHERE id = ?`, id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.Job{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.Job{}, fmt.Errorf("sqlite: getting job %s: %w", id, err)
	}
	return job, nil
}

func (s *JobStore) List(ctx context.Context, filter storage.JobFilter) ([]storage.Job, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := `SELECT ` + selectJobColumns + ` FROM jobs WHERE 1 = 1`
	var args []any
	if filter.OwnerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, filter.OwnerID)
	}
	if filter.WorkspaceID != "" {
		query += ` AND workspace_id = ?`
		args = append(args, filter.WorkspaceID)
	}
	if filter.Type != "" {
		query += ` AND type = ?`
		args = append(args, string(filter.Type))
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing jobs: %w", err)
	}
	defer rows.Close()
	var out []storage.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *JobStore) ListReconcilable(ctx context.Context) ([]storage.Job, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT `+selectJobColumns+` FROM jobs WHERE status IN (?, ?) ORDER BY created_at ASC`,
		string(storage.JobStatusQueued), string(storage.JobStatusRunning))
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing reconcilable jobs: %w", err)
	}
	defer rows.Close()
	var out []storage.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *JobStore) UpdateStatus(ctx context.Context, id string, status storage.JobStatus, reasonCode, errorMessage string, startedAt, completedAt *time.Time) error {
	return execOne(ctx, s.db, `
		UPDATE jobs SET status = ?, reason_code = ?, error_message = ?, started_at = COALESCE(?, started_at),
			completed_at = COALESCE(?, completed_at), updated_at = ?
		WHERE id = ?`, string(status), nullableString(reasonCode), nullableString(errorMessage), formatTimePtr(startedAt), formatTimePtr(completedAt), time.Now().UTC().Format(time.RFC3339Nano), id)
}

func (s *JobStore) AddStep(ctx context.Context, step storage.JobStep) error {
	now := time.Now().UTC()
	if step.CreatedAt.IsZero() {
		step.CreatedAt = now
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO job_steps (id, job_id, name, status, message, created_at, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, step.ID, step.JobID, step.Name, string(step.Status), nullableString(step.Message), step.CreatedAt.Format(time.RFC3339Nano), formatTimePtr(step.StartedAt), formatTimePtr(step.CompletedAt))
	if isUniqueConstraintErr(err) {
		return storage.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("sqlite: adding job step %s: %w", step.ID, err)
	}
	return nil
}

func (s *JobStore) ListSteps(ctx context.Context, jobID string) ([]storage.JobStep, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT id, job_id, name, status, message, created_at, started_at, completed_at FROM job_steps WHERE job_id = ? ORDER BY created_at ASC`, jobID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing job steps: %w", err)
	}
	defer rows.Close()
	var out []storage.JobStep
	for rows.Next() {
		var step storage.JobStep
		var status, createdAt string
		var msg, startedAt, completedAt sql.NullString
		if err := rows.Scan(&step.ID, &step.JobID, &step.Name, &status, &msg, &createdAt, &startedAt, &completedAt); err != nil {
			return nil, err
		}
		step.Status = storage.JobStatus(status)
		step.Message = msg.String
		var parseErr error
		if step.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt); parseErr != nil {
			return nil, parseErr
		}
		if startedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, startedAt.String)
			if err != nil {
				return nil, err
			}
			step.StartedAt = &t
		}
		if completedAt.Valid {
			t, err := time.Parse(time.RFC3339Nano, completedAt.String)
			if err != nil {
				return nil, err
			}
			step.CompletedAt = &t
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

func execOne(ctx context.Context, db *DB, query string, args ...any) error {
	res, err := db.q(ctx).ExecContext(ctx, query, args...)
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
