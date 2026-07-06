package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"appliance-code/services/controlplane/internal/storage"
)

// UserStore is the SQLite-backed storage.UserStore.
type UserStore struct {
	db *DB
}

// NewUserStore returns a UserStore backed by db.
func NewUserStore(db *DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Create(ctx context.Context, u storage.User) error {
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	if u.CredentialVersion == 0 {
		u.CredentialVersion = 1
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO users (id, username, display_name, state, credential_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.Username, u.DisplayName, string(u.State), u.CredentialVersion,
		u.CreatedAt.Format(time.RFC3339Nano), u.UpdatedAt.Format(time.RFC3339Nano),
	)
	if isUniqueConstraintErr(err) {
		return storage.ErrConflict
	}
	if err != nil {
		return fmt.Errorf("sqlite: creating user %s: %w", u.Username, err)
	}
	return nil
}

func scanUser(row interface {
	Scan(dest ...any) error
}) (storage.User, error) {
	var (
		u                    storage.User
		state                string
		createdAt, updatedAt string
	)
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &state, &u.CredentialVersion, &createdAt, &updatedAt); err != nil {
		return storage.User{}, err
	}
	u.State = storage.UserState(state)
	var err error
	if u.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.User{}, err
	}
	if u.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return storage.User{}, err
	}
	return u, nil
}

const selectUserColumns = `id, username, display_name, state, credential_version, created_at, updated_at`

func (s *UserStore) Get(ctx context.Context, id string) (storage.User, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectUserColumns+` FROM users WHERE id = ?`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.User{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.User{}, fmt.Errorf("sqlite: getting user %s: %w", id, err)
	}
	return u, nil
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (storage.User, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `SELECT `+selectUserColumns+` FROM users WHERE username = ?`, username)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return storage.User{}, storage.ErrNotFound
	}
	if err != nil {
		return storage.User{}, fmt.Errorf("sqlite: getting user by username %s: %w", username, err)
	}
	return u, nil
}

func (s *UserStore) List(ctx context.Context) ([]storage.User, error) {
	rows, err := s.db.q(ctx).QueryContext(ctx, `SELECT `+selectUserColumns+` FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: listing users: %w", err)
	}
	defer rows.Close()

	var users []storage.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: scanning user row: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *UserStore) UpdateDisplayName(ctx context.Context, id, displayName string) error {
	return s.exec1(ctx, `UPDATE users SET display_name = ?, updated_at = ? WHERE id = ?`,
		displayName, time.Now().UTC().Format(time.RFC3339Nano), id)
}

func (s *UserStore) SetState(ctx context.Context, id string, state storage.UserState) error {
	return s.exec1(ctx, `UPDATE users SET state = ?, updated_at = ? WHERE id = ?`,
		string(state), time.Now().UTC().Format(time.RFC3339Nano), id)
}

func (s *UserStore) BumpCredentialVersion(ctx context.Context, id string) error {
	return s.exec1(ctx, `UPDATE users SET credential_version = credential_version + 1, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
}

func (s *UserStore) exec1(ctx context.Context, query string, args ...any) error {
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

// CountEnabledAdministrators counts active users holding adminRoleID, used
// to enforce the last-effective-administrator invariant.
func (s *UserStore) CountEnabledAdministrators(ctx context.Context, adminRoleID string) (int, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT COUNT(*) FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		WHERE ur.role_id = ? AND u.state = ?`, adminRoleID, string(storage.UserStateActive))
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: counting enabled administrators: %w", err)
	}
	return n, nil
}

func (s *UserStore) SetPassword(ctx context.Context, cred storage.PasswordCredential) error {
	now := time.Now().UTC()
	if cred.UpdatedAt.IsZero() {
		cred.UpdatedAt = now
	}
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO password_credentials (user_id, algorithm, params, salt, hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET
			algorithm = excluded.algorithm,
			params = excluded.params,
			salt = excluded.salt,
			hash = excluded.hash,
			updated_at = excluded.updated_at`,
		cred.UserID, cred.Algorithm, cred.Params, cred.Salt, cred.Hash, cred.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: setting password for user %s: %w", cred.UserID, err)
	}
	return nil
}

func (s *UserStore) GetPasswordCredential(ctx context.Context, userID string) (storage.PasswordCredential, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT user_id, algorithm, params, salt, hash, updated_at
		FROM password_credentials WHERE user_id = ?`, userID)

	var (
		cred      storage.PasswordCredential
		updatedAt string
	)
	if err := row.Scan(&cred.UserID, &cred.Algorithm, &cred.Params, &cred.Salt, &cred.Hash, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.PasswordCredential{}, storage.ErrNotFound
		}
		return storage.PasswordCredential{}, fmt.Errorf("sqlite: getting password credential for user %s: %w", userID, err)
	}
	var err error
	if cred.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return storage.PasswordCredential{}, err
	}
	return cred, nil
}

func (s *UserStore) CreatePasswordReset(ctx context.Context, cred storage.PasswordResetCredential) error {
	_, err := s.db.q(ctx).ExecContext(ctx, `
		INSERT INTO password_reset_credentials (id, user_id, lookup_id, digest, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		cred.ID, cred.UserID, cred.LookupID, cred.Digest,
		cred.CreatedAt.Format(time.RFC3339Nano), cred.ExpiresAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlite: creating password reset for user %s: %w", cred.UserID, err)
	}
	return nil
}

func (s *UserStore) GetPasswordResetByLookupID(ctx context.Context, lookupID string) (storage.PasswordResetCredential, error) {
	row := s.db.q(ctx).QueryRowContext(ctx, `
		SELECT id, user_id, lookup_id, digest, created_at, expires_at, used_at
		FROM password_reset_credentials WHERE lookup_id = ?`, lookupID)

	var (
		cred                 storage.PasswordResetCredential
		createdAt, expiresAt string
		usedAt               sql.NullString
	)
	if err := row.Scan(&cred.ID, &cred.UserID, &cred.LookupID, &cred.Digest, &createdAt, &expiresAt, &usedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storage.PasswordResetCredential{}, storage.ErrNotFound
		}
		return storage.PasswordResetCredential{}, fmt.Errorf("sqlite: getting password reset %s: %w", lookupID, err)
	}
	var err error
	if cred.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return storage.PasswordResetCredential{}, err
	}
	if cred.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiresAt); err != nil {
		return storage.PasswordResetCredential{}, err
	}
	if usedAt.Valid {
		t, err := time.Parse(time.RFC3339Nano, usedAt.String)
		if err != nil {
			return storage.PasswordResetCredential{}, err
		}
		cred.UsedAt = &t
	}
	return cred, nil
}

func (s *UserStore) MarkPasswordResetUsed(ctx context.Context, id string) error {
	return s.exec1(ctx, `UPDATE password_reset_credentials SET used_at = ? WHERE id = ? AND used_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
}

func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
