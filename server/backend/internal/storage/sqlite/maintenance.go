package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/server/backend/internal/storage"
)

// MaintenanceStore is the SQLite-backed storage.MaintenanceStore.
type MaintenanceStore struct {
	db *DB
}

// NewMaintenanceStore returns a MaintenanceStore backed by db.
func NewMaintenanceStore(db *DB) *MaintenanceStore {
	return &MaintenanceStore{db: db}
}

func (s *MaintenanceStore) Get(ctx context.Context, taskName string) (storage.MaintenanceCheckpoint, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT task_name, last_run_at, cursor, updated_at
		FROM maintenance_checkpoints WHERE task_name = ?`, taskName)

	var (
		cp                storage.MaintenanceCheckpoint
		lastRunAt, cursor sql.NullString
		updatedAt         string
	)
	if err := row.Scan(&cp.TaskName, &lastRunAt, &cursor, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.MaintenanceCheckpoint{}, storage.ErrNotFound
		}
		return storage.MaintenanceCheckpoint{}, fmt.Errorf("sqlite: getting maintenance checkpoint %s: %w", taskName, err)
	}

	cp.Cursor = cursor.String
	if lastRunAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastRunAt.String)
		if err != nil {
			return storage.MaintenanceCheckpoint{}, fmt.Errorf("sqlite: parsing last_run_at for %s: %w", taskName, err)
		}
		cp.LastRunAt = t
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return storage.MaintenanceCheckpoint{}, fmt.Errorf("sqlite: parsing updated_at for %s: %w", taskName, err)
	}
	cp.UpdatedAt = updated
	return cp, nil
}

func (s *MaintenanceStore) Save(ctx context.Context, cp storage.MaintenanceCheckpoint) error {
	cp.UpdatedAt = time.Now().UTC()
	var lastRunAt any
	if !cp.LastRunAt.IsZero() {
		lastRunAt = cp.LastRunAt.Format(time.RFC3339Nano)
	}

	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO maintenance_checkpoints (task_name, last_run_at, cursor, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (task_name) DO UPDATE SET
			last_run_at = excluded.last_run_at,
			cursor = excluded.cursor,
			updated_at = excluded.updated_at`,
		cp.TaskName, lastRunAt, nullableString(cp.Cursor), cp.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: saving maintenance checkpoint %s: %w", cp.TaskName, err)
	}
	return nil
}
