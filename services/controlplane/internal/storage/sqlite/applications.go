package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

type applicationStore struct{ db *DB }

func NewApplicationStore(db *DB) storage.ApplicationStore { return &applicationStore{db: db} }

func (s *applicationStore) UpsertDefinition(ctx context.Context, d storage.ApplicationDefinition) error {
	if _, err := s.db.sqlDB.ExecContext(ctx, `
        INSERT INTO application_definitions(name, version, document, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(name, version) DO UPDATE SET document=excluded.document, updated_at=excluded.updated_at`,
		d.Name, d.Version, d.Document, d.CreatedAt.UTC().Format(time.RFC3339Nano), d.UpdatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("sqlite: upserting application definition: %w", err)
	}
	return nil
}

func (s *applicationStore) GetDefinition(ctx context.Context, name, version string) (storage.ApplicationDefinition, error) {
	var d storage.ApplicationDefinition
	var created, updated string
	err := s.db.sqlDB.QueryRowContext(ctx, `SELECT name, version, document, created_at, updated_at FROM application_definitions WHERE name=? AND version=?`, name, version).
		Scan(&d.Name, &d.Version, &d.Document, &created, &updated)
	if err != nil {
		if err == sql.ErrNoRows {
			return storage.ApplicationDefinition{}, storage.ErrNotFound
		}
		return storage.ApplicationDefinition{}, fmt.Errorf("sqlite: getting application definition: %w", err)
	}
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return d, nil
}

func (s *applicationStore) ListDefinitions(ctx context.Context) ([]storage.ApplicationDefinition, error) {
	rows, err := s.db.sqlDB.QueryContext(ctx, `SELECT name, version, document, created_at, updated_at FROM application_definitions ORDER BY name, version`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing application definitions: %w", err)
	}
	defer rows.Close()
	var result []storage.ApplicationDefinition
	for rows.Next() {
		var d storage.ApplicationDefinition
		var created, updated string
		if err := rows.Scan(&d.Name, &d.Version, &d.Document, &created, &updated); err != nil {
			return nil, fmt.Errorf("sqlite: scanning application definition: %w", err)
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *applicationStore) UpsertInstance(ctx context.Context, i storage.ApplicationInstance) error {
	_, err := s.db.sqlDB.ExecContext(ctx, `
        INSERT INTO application_instances(name, definition_name, definition_version, desired_state, observed_state, message, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(name) DO UPDATE SET definition_name=excluded.definition_name, definition_version=excluded.definition_version,
          desired_state=excluded.desired_state, observed_state=excluded.observed_state, message=excluded.message, updated_at=excluded.updated_at`,
		i.Name, i.DefinitionName, i.DefinitionVersion, i.DesiredState, i.ObservedState, i.Message,
		i.CreatedAt.UTC().Format(time.RFC3339Nano), i.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("sqlite: upserting application instance: %w", err)
	}
	return nil
}

func (s *applicationStore) UpdateInstanceStatus(ctx context.Context, name, observedState, message string, updatedAt time.Time) error {
	result, err := s.db.sqlDB.ExecContext(ctx, `UPDATE application_instances SET observed_state=?, message=?, updated_at=? WHERE name=?`, observedState, message, updatedAt.UTC().Format(time.RFC3339Nano), name)
	if err != nil {
		return fmt.Errorf("sqlite: updating application instance status: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlite: checking application instance status update: %w", err)
	}
	if count == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *applicationStore) GetInstance(ctx context.Context, name string) (storage.ApplicationInstance, error) {
	var i storage.ApplicationInstance
	var created, updated string
	err := s.db.sqlDB.QueryRowContext(ctx, `SELECT name, definition_name, definition_version, desired_state, observed_state, message, created_at, updated_at FROM application_instances WHERE name=?`, name).
		Scan(&i.Name, &i.DefinitionName, &i.DefinitionVersion, &i.DesiredState, &i.ObservedState, &i.Message, &created, &updated)
	if err != nil {
		if err == sql.ErrNoRows {
			return storage.ApplicationInstance{}, storage.ErrNotFound
		}
		return storage.ApplicationInstance{}, fmt.Errorf("sqlite: getting application instance: %w", err)
	}
	i.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	i.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return i, nil
}

func (s *applicationStore) ListInstances(ctx context.Context) ([]storage.ApplicationInstance, error) {
	rows, err := s.db.sqlDB.QueryContext(ctx, `SELECT name, definition_name, definition_version, desired_state, observed_state, message, created_at, updated_at FROM application_instances ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing application instances: %w", err)
	}
	defer rows.Close()
	var result []storage.ApplicationInstance
	for rows.Next() {
		var i storage.ApplicationInstance
		var created, updated string
		if err := rows.Scan(&i.Name, &i.DefinitionName, &i.DefinitionVersion, &i.DesiredState, &i.ObservedState, &i.Message, &created, &updated); err != nil {
			return nil, fmt.Errorf("sqlite: scanning application instance: %w", err)
		}
		i.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		i.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		result = append(result, i)
	}
	return result, rows.Err()
}
