package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// SessionStore is the SQLite-backed storage.SessionStore.
type SessionStore struct {
	db *DB
}

// NewSessionStore returns a SessionStore backed by db.
func NewSessionStore(db *DB) *SessionStore {
	return &SessionStore{db: db}
}

func (s *SessionStore) CreateFamily(ctx context.Context, family storage.SessionFamily, refresh storage.RefreshCredential) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		_, err := s.db.q(ctx).ExecContext(ctx, `
			INSERT INTO session_families (id, user_id, created_at, last_used_at, absolute_expires_at)
			VALUES (?, ?, ?, ?, ?)`,
			family.ID, family.UserID,
			family.CreatedAt.Format(time.RFC3339Nano), family.LastUsedAt.Format(time.RFC3339Nano),
			family.AbsoluteExpiresAt.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("sqlite: creating session family %s: %w", family.ID, err)
		}

		_, err = s.db.q(ctx).ExecContext(ctx, `
			INSERT INTO refresh_credentials (family_id, current_digest, version, expires_at, rotated_at)
			VALUES (?, ?, ?, ?, ?)`,
			refresh.FamilyID, refresh.CurrentDigest, refresh.Version,
			refresh.ExpiresAt.Format(time.RFC3339Nano), refresh.RotatedAt.Format(time.RFC3339Nano),
		)
		if err != nil {
			return fmt.Errorf("sqlite: creating refresh credential for family %s: %w", family.ID, err)
		}
		return nil
	})
}

func scanSessionFamily(row interface{ Scan(dest ...any) error }) (storage.SessionFamily, error) {
	var (
		f                                      storage.SessionFamily
		createdAt, lastUsedAt, absoluteExpires string
		revokedAt, revokedReason               sql.NullString
	)
	if err := row.Scan(&f.ID, &f.UserID, &createdAt, &lastUsedAt, &absoluteExpires, &revokedAt, &revokedReason); err != nil {
		return storage.SessionFamily{}, err
	}
	var err error
	if f.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.SessionFamily{}, err
	}
	if f.LastUsedAt, err = time.Parse(time.RFC3339Nano, lastUsedAt); err != nil {
		return storage.SessionFamily{}, err
	}
	if f.AbsoluteExpiresAt, err = time.Parse(time.RFC3339Nano, absoluteExpires); err != nil {
		return storage.SessionFamily{}, err
	}
	if revokedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, revokedAt.String)
		if err != nil {
			return storage.SessionFamily{}, err
		}
		f.RevokedAt = &t
	}
	f.RevokedReason = revokedReason.String
	return f, nil
}

const selectSessionFamilyColumns = `id, user_id, created_at, last_used_at, absolute_expires_at, revoked_at, revoked_reason`

func (s *SessionStore) GetFamily(ctx context.Context, id string) (storage.SessionFamily, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectSessionFamilyColumns+` FROM session_families WHERE id = ?`, id)
	f, err := scanSessionFamily(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.SessionFamily{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.SessionFamily{}, fmt.Errorf("sqlite: getting session family %s: %w", id, err)
	}
	return f, nil
}

func (s *SessionStore) ListActiveFamiliesForUser(ctx context.Context, userID string) ([]storage.SessionFamily, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `
		SELECT `+selectSessionFamilyColumns+` FROM session_families
		WHERE user_id = ? AND revoked_at IS NULL
		ORDER BY last_used_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing active session families for user %s: %w", userID, err)
	}
	defer rows.Close()

	var families []storage.SessionFamily
	for rows.Next() {
		f, err := scanSessionFamily(rows)
		if err != nil {
			return nil, err
		}
		families = append(families, f)
	}
	return families, rows.Err()
}

func (s *SessionStore) RevokeFamily(ctx context.Context, id, reason string) error {
	res, err := s.db.q(ctx).ExecContext(ctx,
		`UPDATE session_families SET revoked_at = ?, revoked_reason = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), reason, id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: revoking session family %s: %w", id, err)
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

func (s *SessionStore) RevokeAllFamiliesForUser(ctx context.Context, userID, reason string) error {
	_, err := s.db.q(ctx).ExecContext(ctx,
		`UPDATE session_families SET revoked_at = ?, revoked_reason = ? WHERE user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), reason, userID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: revoking session families for user %s: %w", userID, err)
	}
	return nil
}

func (s *SessionStore) TouchFamily(ctx context.Context, id string, lastUsedAt time.Time) error {
	_, err := s.db.q(ctx).ExecContext(ctx,
		`UPDATE session_families SET last_used_at = ? WHERE id = ?`, lastUsedAt.UTC().Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: touching session family %s: %w", id, err)
	}
	return nil
}

func (s *SessionStore) GetRefresh(ctx context.Context, familyID string) (storage.RefreshCredential, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT family_id, current_digest, previous_digest, version, expires_at, rotated_at
		FROM refresh_credentials WHERE family_id = ?`, familyID)

	var (
		rc                   storage.RefreshCredential
		previousDigest       []byte
		expiresAt, rotatedAt string
	)
	if err := row.Scan(&rc.FamilyID, &rc.CurrentDigest, &previousDigest, &rc.Version, &expiresAt, &rotatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.RefreshCredential{}, storage.ErrNotFound
		}
		return storage.RefreshCredential{}, fmt.Errorf("sqlite: getting refresh credential for family %s: %w", familyID, err)
	}
	rc.PreviousDigest = previousDigest
	var err error
	if rc.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt); err != nil {
		return storage.RefreshCredential{}, err
	}
	if rc.RotatedAt, err = time.Parse(time.RFC3339Nano, rotatedAt); err != nil {
		return storage.RefreshCredential{}, err
	}
	return rc, nil
}

func (s *SessionStore) RotateRefresh(ctx context.Context, familyID string, newDigest []byte, expiresAt time.Time) error {
	res, err := s.db.q(ctx).ExecContext(ctx, `
		UPDATE refresh_credentials
		SET previous_digest = current_digest,
		    current_digest = ?,
		    version = version + 1,
		    expires_at = ?,
		    rotated_at = ?
		WHERE family_id = ?`,
		newDigest, expiresAt.Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), familyID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: rotating refresh credential for family %s: %w", familyID, err)
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
