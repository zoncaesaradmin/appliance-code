package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// RegistryGrantStore is the SQLite-backed storage.RegistryGrantStore.
type RegistryGrantStore struct {
	db *DB
}

// NewRegistryGrantStore returns a RegistryGrantStore backed by db.
func NewRegistryGrantStore(db *DB) *RegistryGrantStore {
	return &RegistryGrantStore{db: db}
}

func (s *RegistryGrantStore) Create(ctx context.Context, g storage.RegistryGrant) error {
	actionsJSON, err := json.Marshal(g.Actions)
	if err != nil {
		return fmt.Errorf("sqlite: encoding registry grant actions: %w", err)
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}
	_, err = s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO registry_grants (id, subject_type, subject_id, path_prefix, actions, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		g.ID, string(g.SubjectType), g.SubjectID, g.PathPrefix, actionsJSON, g.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: creating registry grant %s: %w", g.ID, err)
	}
	return nil
}

const selectRegistryGrantColumns = `id, subject_type, subject_id, path_prefix, actions, created_at`

func scanRegistryGrant(row interface{ Scan(dest ...any) error }) (storage.RegistryGrant, error) {
	var (
		g           storage.RegistryGrant
		subjectType string
		actionsRaw  []byte
		createdAt   string
	)
	if err := row.Scan(&g.ID, &subjectType, &g.SubjectID, &g.PathPrefix, &actionsRaw, &createdAt); err != nil {
		return storage.RegistryGrant{}, err
	}
	g.SubjectType = storage.RegistryGrantSubjectType(subjectType)
	if err := json.Unmarshal(actionsRaw, &g.Actions); err != nil {
		return storage.RegistryGrant{}, fmt.Errorf("sqlite: decoding registry grant actions: %w", err)
	}
	var err error
	if g.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.RegistryGrant{}, err
	}
	return g, nil
}

func (s *RegistryGrantStore) Get(ctx context.Context, id string) (storage.RegistryGrant, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectRegistryGrantColumns+` FROM registry_grants WHERE id = ?`, id)
	g, err := scanRegistryGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.RegistryGrant{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.RegistryGrant{}, fmt.Errorf("sqlite: getting registry grant %s: %w", id, err)
	}
	return g, nil
}

func (s *RegistryGrantStore) List(ctx context.Context) ([]storage.RegistryGrant, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT `+selectRegistryGrantColumns+` FROM registry_grants ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing registry grants: %w", err)
	}
	defer rows.Close()

	var grants []storage.RegistryGrant
	for rows.Next() {
		g, err := scanRegistryGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (s *RegistryGrantStore) ListForSubjects(ctx context.Context, userID string, roleIDs []string) ([]storage.RegistryGrant, error) {
	query := `SELECT ` + selectRegistryGrantColumns + ` FROM registry_grants WHERE (subject_type = 'user' AND subject_id = ?)`
	args := []any{userID}
	if len(roleIDs) > 0 {
		placeholders := make([]string, len(roleIDs))
		for i, id := range roleIDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		query += ` OR (subject_type = 'role' AND subject_id IN (` + strings.Join(placeholders, ",") + `))`
	}

	rows, err := s.db.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing registry grants for subjects: %w", err)
	}
	defer rows.Close()

	var grants []storage.RegistryGrant
	for rows.Next() {
		g, err := scanRegistryGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (s *RegistryGrantStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.q(ctx).ExecContext(ctx, `DELETE FROM registry_grants WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("sqlite: deleting registry grant %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}
