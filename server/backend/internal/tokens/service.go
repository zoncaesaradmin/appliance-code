// Package tokens owns API token lifecycle business logic above
// storage.TokenStore: issuance within the ADR 0010 lifetime bounds,
// listing, revocation, and authentication of a presented raw token.
package tokens

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"appliance-code/server/backend/internal/audit"
	"appliance-code/server/backend/internal/authn"
	"appliance-code/server/backend/internal/keys"
	"appliance-code/server/backend/internal/storage"
)

const (
	DefaultLifetime = 90 * 24 * time.Hour
	MaxLifetime     = 365 * 24 * time.Hour
	MinLifetime     = 1 * time.Hour
)

// ErrInvalidLifetime is returned when a requested token lifetime falls
// outside [MinLifetime, MaxLifetime].
var ErrInvalidLifetime = errors.New("tokens: lifetime must be between 1 hour and 365 days")

// ErrInvalidToken covers every way a presented raw token can fail to
// authenticate: malformed, unknown, revoked, or expired. The specific cause
// is deliberately not distinguished to callers.
var ErrInvalidToken = errors.New("tokens: invalid token")

// Service implements API token lifecycle business logic.
type Service struct {
	db     storage.DB
	tokens storage.TokenStore
	audit  *audit.Recorder
	keys   *keys.Material
}

// NewService wires a Service from its storage and shared-secret
// dependencies.
func NewService(db storage.DB, tokens storage.TokenStore, recorder *audit.Recorder, keyMaterial *keys.Material) *Service {
	return &Service{db: db, tokens: tokens, audit: recorder, keys: keyMaterial}
}

// Create issues a new API token for ownerUserID. A zero ttl selects
// DefaultLifetime; scopes may reduce, but never expand, the owner's
// effective permissions and a nil scopes means "inherit all."
func (s *Service) Create(ctx context.Context, actor audit.Actor, ownerUserID, name string, ttl time.Duration, scopes []string) (raw string, token storage.APIToken, err error) {
	if ttl == 0 {
		ttl = DefaultLifetime
	}
	if ttl < MinLifetime || ttl > MaxLifetime {
		return "", storage.APIToken{}, ErrInvalidLifetime
	}

	raw, lookupID, secret, err := authn.GenerateAPIToken()
	if err != nil {
		return "", storage.APIToken{}, err
	}
	digest := authn.DigestAPITokenSecret(secret, s.keys.APITokenPepper)

	now := time.Now().UTC()
	token = storage.APIToken{
		ID: uuid.Must(uuid.NewV7()).String(), UserID: ownerUserID, Name: name,
		LookupID: lookupID, Digest: digest, Scopes: scopes,
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	}

	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.tokens.Create(ctx, token); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "tokens.create", TargetType: "api_token", TargetID: token.ID, Outcome: storage.AuditOutcomeSuccess,
			Details: map[string]any{"name": name, "ownerUserId": ownerUserID},
		})
	})
	if errors.Is(err, storage.ErrConflict) {
		return "", storage.APIToken{}, fmt.Errorf("tokens: token id collision, retry")
	}
	if err != nil {
		return "", storage.APIToken{}, err
	}
	return raw, token, nil
}

func (s *Service) ListByUser(ctx context.Context, userID string) ([]storage.APIToken, error) {
	return s.tokens.ListByUser(ctx, userID)
}

func (s *Service) Get(ctx context.Context, id string) (storage.APIToken, error) {
	return s.tokens.Get(ctx, id)
}

// Revoke immediately invalidates an API token. Revocation is effective at
// the control plane immediately; any already-issued zot registry token
// still expires within its own five-minute lifetime.
func (s *Service) Revoke(ctx context.Context, actor audit.Actor, id string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.tokens.Revoke(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "tokens.revoke", TargetType: "api_token", TargetID: id, Outcome: storage.AuditOutcomeSuccess,
		})
	})
}

// Authenticate validates a presented raw API token and returns the
// underlying record if it is unexpired and unrevoked. last_used_at is
// updated best-effort: a failure to record it never fails authentication,
// per ADR 0010.
func (s *Service) Authenticate(ctx context.Context, raw string) (storage.APIToken, error) {
	lookupID, secret, err := authn.ParseAPIToken(raw)
	if err != nil {
		return storage.APIToken{}, ErrInvalidToken
	}

	token, err := s.tokens.GetByLookupID(ctx, lookupID)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.APIToken{}, ErrInvalidToken
	}
	if err != nil {
		return storage.APIToken{}, err
	}

	if token.RevokedAt != nil {
		return storage.APIToken{}, ErrInvalidToken
	}
	if time.Now().UTC().After(token.ExpiresAt) {
		return storage.APIToken{}, ErrInvalidToken
	}
	if !authn.VerifyAPITokenDigest(secret, s.keys.APITokenPepper, token.Digest) {
		return storage.APIToken{}, ErrInvalidToken
	}

	_ = s.tokens.TouchLastUsed(ctx, token.ID, time.Now().UTC())
	return token, nil
}
