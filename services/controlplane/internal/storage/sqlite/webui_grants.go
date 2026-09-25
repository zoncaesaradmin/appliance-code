package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// WebUILaunchGrantStore persists one-time browser handoffs independently of
// process memory so a control-plane restart cannot resurrect a consumed grant.
type WebUILaunchGrantStore struct{ db *DB }

func NewWebUILaunchGrantStore(db *DB) *WebUILaunchGrantStore { return &WebUILaunchGrantStore{db: db} }

func (s *WebUILaunchGrantStore) CreateWebUILaunchGrant(ctx context.Context, g storage.WebUILaunchGrant) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `INSERT INTO webui_launch_grants
      (id, lookup_id, digest, user_id, family_id, created_at, expires_at)
      VALUES (?, ?, ?, ?, ?, ?, ?)`, g.ID, g.LookupID, g.Digest, g.UserID, g.FamilyID,
		g.CreatedAt.UTC().Format(time.RFC3339Nano), g.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("sqlite: create WebUI launch grant: %w", err)
	}
	return nil
}

func (s *WebUILaunchGrantStore) GetWebUILaunchGrantByLookupID(ctx context.Context, lookupID string) (storage.WebUILaunchGrant, error) {
	var g storage.WebUILaunchGrant
	var created, expires string
	var used sql.NullString
	err := s.db.q(ctx).QueryRowContext(ctx, `SELECT id, lookup_id, digest, user_id, family_id, created_at, expires_at, used_at
      FROM webui_launch_grants WHERE lookup_id = ?`, lookupID).Scan(&g.ID, &g.LookupID, &g.Digest, &g.UserID, &g.FamilyID, &created, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.WebUILaunchGrant{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.WebUILaunchGrant{}, fmt.Errorf("sqlite: get WebUI launch grant: %w", err)
	}
	var parseErr error
	if g.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, created); parseErr != nil {
		return storage.WebUILaunchGrant{}, parseErr
	}
	if g.ExpiresAt, parseErr = time.Parse(time.RFC3339Nano, expires); parseErr != nil {
		return storage.WebUILaunchGrant{}, parseErr
	}
	if used.Valid {
		t, err := time.Parse(time.RFC3339Nano, used.String)
		if err != nil {
			return storage.WebUILaunchGrant{}, err
		}
		g.UsedAt = &t
	}
	return g, nil
}

func (s *WebUILaunchGrantStore) ConsumeWebUILaunchGrant(ctx context.Context, id string, usedAt time.Time) error {
	result, err := s.db.q(ctx).ExecContext(ctx, `UPDATE webui_launch_grants SET used_at = ? WHERE id = ? AND used_at IS NULL`, usedAt.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("sqlite: consume WebUI launch grant: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return storage.ErrNotFound
	}
	return nil
}
