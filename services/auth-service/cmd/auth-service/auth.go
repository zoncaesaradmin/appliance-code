package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sync"
	"time"
)

var (
	errInvalidPhone     = errors.New("invalid phone number")
	errInvalidChallenge = errors.New("invalid or expired challenge")
	errRateLimited      = errors.New("too many authentication attempts")
	errDelivery         = errors.New("verification delivery unavailable")
	phonePattern        = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	codePattern         = regexp.MustCompile(`^[0-9]{6}$`)
	challengePattern    = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

const authSchemaSQL = `
CREATE TABLE IF NOT EXISTS identity_users (id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'), phone text NOT NULL UNIQUE CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'), onboarding_state text NOT NULL DEFAULT 'identified' CHECK (onboarding_state IN ('identified', 'join_pending', 'onboarded', 'suspended')), created_at timestamptz NOT NULL);
ALTER TABLE identity_users ADD COLUMN IF NOT EXISTS onboarding_state text NOT NULL DEFAULT 'identified';
CREATE TABLE IF NOT EXISTS identity_tenants (id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9.-]{1,62}$'), display_name text NOT NULL, status text NOT NULL CHECK (status IN ('active', 'suspended')), created_at timestamptz NOT NULL);
INSERT INTO identity_tenants (id, display_name, status, created_at) VALUES ('zonited-solutions', 'Zonited Solutions', 'active', CURRENT_TIMESTAMP) ON CONFLICT (id) DO NOTHING;
CREATE TABLE IF NOT EXISTS identity_channels (id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9.-]{1,62}$'), tenant_id text NOT NULL REFERENCES identity_tenants (id), audience text NOT NULL CHECK (audience IN ('customer', 'vendor', 'zone-admin', 'tenant-admin')), auto_register boolean NOT NULL DEFAULT false, enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS identity_channels_tenant_idx ON identity_channels (tenant_id);
INSERT INTO identity_channels (id, tenant_id, audience, auto_register, enabled, created_at, updated_at) VALUES ('customer-web', 'zonited-solutions', 'customer', true, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (id) DO NOTHING;
INSERT INTO identity_channels (id, tenant_id, audience, auto_register, enabled, created_at, updated_at) VALUES ('customer-whatsapp', 'zonited-solutions', 'customer', true, true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP) ON CONFLICT (id) DO NOTHING;
CREATE TABLE IF NOT EXISTS identity_otp_challenges (id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'), phone text NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{7,14}$'), digest bytea NOT NULL CHECK (octet_length(digest) = 32), expires_at timestamptz NOT NULL, attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0), consumed_at timestamptz);
CREATE INDEX IF NOT EXISTS identity_otp_challenges_expiry_idx ON identity_otp_challenges (expires_at);
CREATE TABLE IF NOT EXISTS identity_otp_issuances (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, phone_key bytea NOT NULL CHECK (octet_length(phone_key) = 32), client_key bytea NOT NULL CHECK (octet_length(client_key) = 32), created_at timestamptz NOT NULL);
CREATE INDEX IF NOT EXISTS identity_otp_issuances_phone_time_idx ON identity_otp_issuances (phone_key, created_at);
CREATE INDEX IF NOT EXISTS identity_otp_issuances_client_time_idx ON identity_otp_issuances (client_key, created_at);
CREATE TABLE IF NOT EXISTS identity_sessions (token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32), user_id text NOT NULL REFERENCES identity_users (id), created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL, revoked_at timestamptz, CHECK (expires_at > created_at));
ALTER TABLE identity_sessions ADD COLUMN IF NOT EXISTS session_id text;
ALTER TABLE identity_sessions ADD COLUMN IF NOT EXISTS tenant_id text NOT NULL DEFAULT 'zonited-solutions';
ALTER TABLE identity_sessions ADD COLUMN IF NOT EXISTS channel_id text NOT NULL DEFAULT 'customer-web';
ALTER TABLE identity_sessions ADD COLUMN IF NOT EXISTS subject_id text;
ALTER TABLE identity_sessions ADD COLUMN IF NOT EXISTS audience text NOT NULL DEFAULT 'customer';
ALTER TABLE identity_sessions ADD COLUMN IF NOT EXISTS auth_method text NOT NULL DEFAULT 'mobile_otp';
CREATE INDEX IF NOT EXISTS identity_sessions_expiry_idx ON identity_sessions (expires_at);`

type config struct {
	environment, address, databaseURL, origin, socketParent  string
	deliveryMode, messagingURL, otpKey, rateKey, deliveryKey string
	deliveryChannels                                         []string
}

type otpRequest struct {
	ChallengeID, Phone, Code, Factor, Purpose, OriginChannel, Audience, TenantID string
	DeliveryChannels                                                             []string
	ExpiresAt                                                                    time.Time
}

type sender interface {
	SendOTP(context.Context, otpRequest) error
}

type authStore struct {
	db      *sql.DB
	rateKey []byte
	now     func() time.Time
}

func newAuthStore(db *sql.DB, key []byte) *authStore {
	return &authStore{db: db, rateKey: append([]byte(nil), key...), now: time.Now}
}
func (s *authStore) digest(kind, value string) []byte {
	mac := hmac.New(sha256.New, s.rateKey)
	_, _ = mac.Write([]byte(kind + ":" + value))
	return mac.Sum(nil)
}
func (s *authStore) createChallenge(ctx context.Context, id, phone string, digest [32]byte, expires time.Time, client string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	phoneKey, clientKey := s.digest("phone", phone), s.digest("client", client)
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_otp_issuances WHERE phone_key=$1 AND created_at >= $2`, phoneKey, s.now().Add(-10*time.Minute)).Scan(&n); err != nil {
		return err
	}
	if n >= 5 {
		return errRateLimited
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM identity_otp_issuances WHERE client_key=$1 AND created_at >= $2`, clientKey, s.now().Add(-10*time.Minute)).Scan(&n); err != nil {
		return err
	}
	if n >= 20 {
		return errRateLimited
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_otp_challenges (id,phone,digest,expires_at,attempts) VALUES ($1,$2,$3,$4,0)`, id, phone, digest[:], expires); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_otp_issuances (phone_key,client_key,created_at) VALUES ($1,$2,$3)`, phoneKey, clientKey, s.now()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *authStore) revokeChallenge(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM identity_otp_challenges WHERE id=$1`, id)
	return err
}
func (s *authStore) verify(ctx context.Context, id string, expected [32]byte, now, sessionExpiry time.Time) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var phone string
	var stored []byte
	var expires time.Time
	var attempts int
	var consumed sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT phone,digest,expires_at,attempts,consumed_at FROM identity_otp_challenges WHERE id=$1 FOR UPDATE`, id).Scan(&phone, &stored, &expires, &attempts, &consumed)
	if errors.Is(err, sql.ErrNoRows) || consumed.Valid || !now.Before(expires) || attempts >= 5 {
		return "", errInvalidChallenge
	}
	if err != nil {
		return "", err
	}
	if !hmac.Equal(stored, expected[:]) {
		if _, err := tx.ExecContext(ctx, `UPDATE identity_otp_challenges SET attempts=attempts+1 WHERE id=$1`, id); err != nil {
			return "", err
		}
		_ = tx.Commit()
		return "", errInvalidChallenge
	}
	userRaw := make([]byte, 16)
	if _, err := rand.Read(userRaw); err != nil {
		return "", err
	}
	var userID string
	if err := tx.QueryRowContext(ctx, `INSERT INTO identity_users (id,phone,created_at) VALUES ($1,$2,$3) ON CONFLICT(phone) DO UPDATE SET phone=EXCLUDED.phone RETURNING id`, hex.EncodeToString(userRaw), phone, now).Scan(&userID); err != nil {
		return "", err
	}
	tokenRaw := make([]byte, 32)
	if _, err := rand.Read(tokenRaw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenRaw)
	if _, err := tx.ExecContext(ctx, `INSERT INTO identity_sessions (token_hash,user_id,created_at,expires_at) VALUES ($1,$2,$3,$4)`, sha256.Sum256([]byte(token)), userID, now, sessionExpiry); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE identity_otp_challenges SET consumed_at=$2 WHERE id=$1`, id, now); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}
func (s *authStore) validSession(ctx context.Context, token string, now time.Time) error {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return errInvalidChallenge
	}
	var id string
	err = s.db.QueryRowContext(ctx, `SELECT user_id FROM identity_sessions WHERE token_hash=$1 AND expires_at>$2 AND revoked_at IS NULL`, sha256.Sum256([]byte(token)), now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return errInvalidChallenge
	}
	return err
}
func (s *authStore) revokeSession(ctx context.Context, token string, now time.Time) error {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil
	}
	_, err = s.db.ExecContext(ctx, `UPDATE identity_sessions SET revoked_at=$2 WHERE token_hash=$1 AND revoked_at IS NULL`, sha256.Sum256([]byte(token)), now)
	return err
}

type localInbox struct {
	mu     sync.Mutex
	values map[string]struct {
		code    string
		expires time.Time
	}
}

func newLocalInbox() *localInbox {
	return &localInbox{values: map[string]struct {
		code    string
		expires time.Time
	}{}}
}
func (i *localInbox) put(phone, code string, expires time.Time) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.values[phone] = struct {
		code    string
		expires time.Time
	}{code, expires}
}
func (i *localInbox) take(phone string) (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	v, ok := i.values[phone]
	if !ok || time.Now().After(v.expires) {
		return "", errors.New("development code unavailable")
	}
	delete(i.values, phone)
	return v.code, nil
}

type localSender struct{ inbox *localInbox }

func (s localSender) SendOTP(_ context.Context, r otpRequest) error {
	s.inbox.put(r.Phone, r.Code, r.ExpiresAt)
	return nil
}

func createChallenge(hmacKey []byte, id, code string) [32]byte {
	mac := hmac.New(sha256.New, hmacKey)
	_, _ = mac.Write([]byte(id + ":" + code))
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}
func newChallengeID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
