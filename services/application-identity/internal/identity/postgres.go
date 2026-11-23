package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"
)

// PostgresLimits defines the rolling issuance window and per-challenge limit.
type PostgresLimits struct {
	Window      time.Duration
	PerPhone    int
	PerClient   int
	MaxAttempts int
}

// PostgresStore persists OTP and session state in the Shared database.
// Its schema and credentials must be provisioned by the migration process.
type PostgresStore struct {
	db     *sql.DB
	key    []byte
	limits PostgresLimits
	now    func() time.Time
}

func NewPostgresStore(db *sql.DB, rateKey []byte, limits PostgresLimits) (*PostgresStore, error) {
	if db == nil || len(rateKey) < 32 || limits.Window <= 0 || limits.PerPhone < 1 || limits.PerClient < 1 || limits.MaxAttempts < 1 {
		return nil, errors.New("invalid postgres identity configuration")
	}
	return &PostgresStore{db: db, key: append([]byte(nil), rateKey...), limits: limits, now: time.Now}, nil
}

func (s *PostgresStore) rateDigest(kind, value string) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(kind + ":" + value))
	return mac.Sum(nil)
}

func (s *PostgresStore) CreateChallenge(ctx context.Context, c Challenge, clientKey string) error {
	if !challengePattern.MatchString(c.ID) || !phonePattern.MatchString(c.Phone) || clientKey == "" || !c.ExpiresAt.After(s.now()) {
		return errors.New("invalid challenge")
	}
	phoneKey := s.rateDigest("phone", c.Phone)
	clientDigest := s.rateDigest("client", clientKey)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin challenge transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Acquire the two rate-bucket locks in stable order to avoid deadlocks.
	keys := []string{"phone:" + hex.EncodeToString(phoneKey), "client:" + hex.EncodeToString(clientDigest)}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "zonited:otp:"+key); err != nil {
			return fmt.Errorf("lock challenge rate bucket: %w", err)
		}
	}
	now := s.now()
	var byPhone, byClient int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_otp_issuances WHERE phone_key = $1 AND created_at >= $2`, phoneKey, now.Add(-s.limits.Window)).Scan(&byPhone); err != nil {
		return fmt.Errorf("check phone issuance limit: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_otp_issuances WHERE client_key = $1 AND created_at >= $2`, clientDigest, now.Add(-s.limits.Window)).Scan(&byClient); err != nil {
		return fmt.Errorf("check client issuance limit: %w", err)
	}
	if byPhone >= s.limits.PerPhone || byClient >= s.limits.PerClient {
		return ErrRateLimited
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_otp_challenges (id, phone, digest, expires_at, attempts) VALUES ($1, $2, $3, $4, 0)`, c.ID, c.Phone, c.Digest[:], c.ExpiresAt); err != nil {
		return fmt.Errorf("insert challenge: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_otp_issuances (phone_key, client_key, created_at) VALUES ($1, $2, $3)`, phoneKey, clientDigest, now); err != nil {
		return fmt.Errorf("record challenge issuance: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit challenge: %w", err)
	}
	return nil
}

func (s *PostgresStore) RevokeChallenge(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM identity_otp_challenges WHERE id = $1`, id); err != nil {
		return fmt.Errorf("revoke challenge: %w", err)
	}
	return nil
}

func (s *PostgresStore) VerifyAndCreateSession(ctx context.Context, id string, digest [32]byte, now time.Time, session Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin verification transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var phone string
	var storedDigest []byte
	var expires time.Time
	var attempts int
	var consumed sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT phone, digest, expires_at, attempts, consumed_at FROM identity_otp_challenges WHERE id = $1 FOR UPDATE`, id).Scan(&phone, &storedDigest, &expires, &attempts, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidChallenge
	}
	if err != nil {
		return fmt.Errorf("load challenge: %w", err)
	}
	if consumed.Valid || !now.Before(expires) || attempts >= s.limits.MaxAttempts {
		return ErrInvalidChallenge
	}
	if !hmac.Equal(storedDigest, digest[:]) {
		if _, err := tx.ExecContext(ctx, `UPDATE identity_otp_challenges SET attempts = attempts + 1 WHERE id = $1`, id); err != nil {
			return fmt.Errorf("record failed verification: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit failed verification: %w", err)
		}
		return ErrInvalidChallenge
	}
	userIDBytes := make([]byte, 16)
	if _, err := rand.Read(userIDBytes); err != nil {
		return fmt.Errorf("generate user id: %w", err)
	}
	var userID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO identity_users (id, phone, created_at) VALUES ($1, $2, $3) ON CONFLICT (phone) DO UPDATE SET phone = EXCLUDED.phone RETURNING id`, hex.EncodeToString(userIDBytes), phone, now).Scan(&userID); err != nil {
		return fmt.Errorf("find or create user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_sessions (token_hash, user_id, expires_at, created_at) VALUES ($1, $2, $3, $4)`, session.TokenHash[:], userID, session.ExpiresAt, now); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE identity_otp_challenges SET consumed_at = $2 WHERE id = $1`, id, now); err != nil {
		return fmt.Errorf("consume challenge: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit verification: %w", err)
	}
	return nil
}

func (s *PostgresStore) LookupSession(ctx context.Context, token [32]byte, now time.Time) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM identity_sessions WHERE token_hash = $1 AND expires_at > $2 AND revoked_at IS NULL`, token[:], now).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnauthenticated
	}
	if err != nil {
		return "", fmt.Errorf("lookup session: %w", err)
	}
	return userID, nil
}

func (s *PostgresStore) RevokeSession(ctx context.Context, token [32]byte, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE identity_sessions SET revoked_at=$2 WHERE token_hash=$1 AND revoked_at IS NULL`, token[:], now); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}
