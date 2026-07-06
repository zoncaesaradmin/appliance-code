package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// TokenStore is the SQLite-backed storage.TokenStore.
type TokenStore struct {
	db *DB
}

// NewTokenStore returns a TokenStore backed by db.
func NewTokenStore(db *DB) *TokenStore {
	return &TokenStore{db: db}
}

func (s *TokenStore) Create(ctx context.Context, t storage.APIToken) error {
	scopesJSON, err := encodeScopes(t.Scopes)
	if err != nil {
		return err
	}
	_, err = s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO api_tokens (id, user_id, name, lookup_id, digest, scopes, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.UserID, t.Name, t.LookupID, t.Digest, scopesJSON,
		t.CreatedAt.Format(time.RFC3339Nano), t.ExpiresAt.Format(time.RFC3339Nano),
	)
	if isUniqueConstraintErr(err) {
		return storage.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating api token %s: %w", t.ID, err)
	}
	return nil
}

func encodeScopes(scopes []string) ([]byte, error) {
	if scopes == nil {
		return nil, nil
	}
	b, err := json.Marshal(scopes)
	if err != nil {
		return nil, fmt.Errorf("sqlite: encoding token scopes: %w", err)
	}
	return b, nil
}

func decodeScopes(raw []byte) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	var scopes []string
	if err := json.Unmarshal(raw, &scopes); err != nil {
		return nil, fmt.Errorf("sqlite: decoding token scopes: %w", err)
	}
	return scopes, nil
}

const selectAPITokenColumns = `id, user_id, name, lookup_id, digest, scopes, created_at, expires_at, last_used_at, revoked_at`

func scanAPIToken(row interface{ Scan(dest ...any) error }) (storage.APIToken, error) {
	var (
		t                     storage.APIToken
		scopesRaw             []byte
		createdAt, expiresAt  string
		lastUsedAt, revokedAt sql.NullString
	)
	if err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.LookupID, &t.Digest, &scopesRaw, &createdAt, &expiresAt, &lastUsedAt, &revokedAt); err != nil {
		return storage.APIToken{}, err
	}

	scopes, err := decodeScopes(scopesRaw)
	if err != nil {
		return storage.APIToken{}, err
	}
	t.Scopes = scopes

	if t.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.APIToken{}, err
	}
	if t.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt); err != nil {
		return storage.APIToken{}, err
	}
	if lastUsedAt.Valid {
		v, err := time.Parse(time.RFC3339Nano, lastUsedAt.String)
		if err != nil {
			return storage.APIToken{}, err
		}
		t.LastUsedAt = &v
	}
	if revokedAt.Valid {
		v, err := time.Parse(time.RFC3339Nano, revokedAt.String)
		if err != nil {
			return storage.APIToken{}, err
		}
		t.RevokedAt = &v
	}
	return t, nil
}

func (s *TokenStore) GetByLookupID(ctx context.Context, lookupID string) (storage.APIToken, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectAPITokenColumns+` FROM api_tokens WHERE lookup_id = ?`, lookupID)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.APIToken{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.APIToken{}, fmt.Errorf("sqlite: getting api token by lookup id: %w", err)
	}
	return t, nil
}

func (s *TokenStore) Get(ctx context.Context, id string) (storage.APIToken, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectAPITokenColumns+` FROM api_tokens WHERE id = ?`, id)
	t, err := scanAPIToken(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.APIToken{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.APIToken{}, fmt.Errorf("sqlite: getting api token %s: %w", id, err)
	}
	return t, nil
}

func (s *TokenStore) ListByUser(ctx context.Context, userID string) ([]storage.APIToken, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT `+selectAPITokenColumns+` FROM api_tokens WHERE user_id = ? ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing api tokens for user %s: %w", userID, err)
	}
	defer rows.Close()

	var tokens []storage.APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (s *TokenStore) Revoke(ctx context.Context, id string) error {
	res, err := s.db.q(ctx).ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: revoking api token %s: %w", id, err)
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

func (s *TokenStore) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := s.db.q(ctx).ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), userID,
	)
	if err != nil {
		return fmt.Errorf("sqlite: revoking api tokens for user %s: %w", userID, err)
	}
	return nil
}

func (s *TokenStore) TouchLastUsed(ctx context.Context, id string, when time.Time) error {
	_, err := s.db.q(ctx).ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, when.UTC().Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return fmt.Errorf("sqlite: touching last_used_at for api token %s: %w", id, err)
	}
	return nil
}
