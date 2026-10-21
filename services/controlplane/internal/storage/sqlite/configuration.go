package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// ConfigurationRevisionStore implements the backend-neutral configuration
// revision contract for both the temporary SQLite test backend and PostgreSQL
// compatibility adapter. The SQL shape is deliberately portable until the
// native PostgreSQL repository package replaces the compatibility adapter.
type ConfigurationRevisionStore struct{ db *DB }

func NewConfigurationRevisionStore(db *DB) *ConfigurationRevisionStore {
	return &ConfigurationRevisionStore{db: db}
}

func (s *ConfigurationRevisionStore) CreateConfigurationRevision(ctx context.Context, revision storage.ConfigurationRevision) error {
	if revision.CreatedAt.IsZero() {
		revision.CreatedAt = time.Now().UTC()
	}
	if revision.Status == "" {
		revision.Status = storage.ConfigurationRevisionPending
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO configuration_revisions
			(id, schema_version, payload, payload_digest, created_at, created_by, status, apply_message, applied_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revision.ID, revision.SchemaVersion, revision.Payload, revision.PayloadDigest,
		revision.CreatedAt.UTC().Format(time.RFC3339Nano), nullIfEmpty(revision.CreatedBy),
		revision.Status, revision.ApplyMessage, formatOptionalTime(revision.AppliedAt),
	)
	if err != nil {
		return fmt.Errorf("sqlite: creating configuration revision %s: %w", revision.ID, err)
	}
	return nil
}

func (s *ConfigurationRevisionStore) GetConfigurationRevision(ctx context.Context, id string) (storage.ConfigurationRevision, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT id, schema_version, payload, payload_digest, created_at,
		       COALESCE(created_by, ''), status, apply_message, applied_at
		FROM configuration_revisions WHERE id = ?`, id)
	revision, err := scanConfigurationRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ConfigurationRevision{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.ConfigurationRevision{}, fmt.Errorf("sqlite: getting configuration revision %s: %w", id, err)
	}
	return revision, nil
}

func (s *ConfigurationRevisionStore) LatestConfigurationRevision(ctx context.Context) (storage.ConfigurationRevision, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT id, schema_version, payload, payload_digest, created_at,
		       COALESCE(created_by, ''), status, apply_message, applied_at
		FROM configuration_revisions ORDER BY created_at DESC, id DESC LIMIT 1`)
	revision, err := scanConfigurationRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.ConfigurationRevision{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.ConfigurationRevision{}, fmt.Errorf("sqlite: getting latest configuration revision: %w", err)
	}
	return revision, nil
}

func (s *ConfigurationRevisionStore) SetConfigurationRevisionStatus(ctx context.Context, id, status, message string, appliedAt *time.Time) error {
	result, err := s.db.q(ctx).ExecContext(ctx, `
		UPDATE configuration_revisions SET status = ?, apply_message = ?, applied_at = ? WHERE id = ?`,
		status, message, formatOptionalTime(appliedAt), id)
	if err != nil {
		return fmt.Errorf("sqlite: updating configuration revision %s: %w", id, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: checking configuration revision update %s: %w", id, err)
	}
	if count == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func scanConfigurationRevision(row interface{ Scan(dest ...any) error }) (storage.ConfigurationRevision, error) {
	var (
		revision  storage.ConfigurationRevision
		createdAt string
		appliedAt sql.NullString
	)
	if err := row.Scan(&revision.ID, &revision.SchemaVersion, &revision.Payload, &revision.PayloadDigest,
		&createdAt, &revision.CreatedBy, &revision.Status, &revision.ApplyMessage, &appliedAt); err != nil {
		return storage.ConfigurationRevision{}, err
	}
	parsedCreatedAt, err := parseTimeFlexible(createdAt)
	if err != nil {
		return storage.ConfigurationRevision{}, fmt.Errorf("sqlite: parsing configuration revision created_at: %w", err)
	}
	revision.CreatedAt = parsedCreatedAt
	if appliedAt.Valid && appliedAt.String != "" {
		parsedAppliedAt, err := parseTimeFlexible(appliedAt.String)
		if err != nil {
			return storage.ConfigurationRevision{}, fmt.Errorf("sqlite: parsing configuration revision applied_at: %w", err)
		}
		revision.AppliedAt = &parsedAppliedAt
	}
	return revision, nil
}

func formatOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
