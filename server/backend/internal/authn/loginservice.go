package authn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"appliance-code/server/backend/internal/audit"
	"appliance-code/server/backend/internal/keys"
	"appliance-code/server/backend/internal/storage"
)

// RefreshIdleLifetime and RefreshAbsoluteLifetime are the fixed ADR 0010
// refresh-credential bounds.
const (
	RefreshIdleLifetime     = 12 * time.Hour
	RefreshAbsoluteLifetime = 7 * 24 * time.Hour
	MaxConcurrentFamilies   = 5

	failureWindowReset   = time.Hour
	lockDuration         = 15 * time.Minute
	lockThreshold        = 20
	progressiveDelayAt   = 5
	progressiveDelayStep = 200 * time.Millisecond
	progressiveDelayMax  = 2 * time.Second
)

// ErrInvalidCredentials covers wrong username, wrong password, unknown
// account, and disabled account uniformly, so responses never reveal which
// case applies.
var ErrInvalidCredentials = errors.New("authn: invalid username or password")

// ErrAccountLocked is returned once an account has crossed the ADR 0010
// failure threshold within the current window.
var ErrAccountLocked = errors.New("authn: account temporarily locked, try again later")

// ErrInvalidRefreshToken covers a malformed, unknown, expired, or revoked
// refresh credential.
var ErrInvalidRefreshToken = errors.New("authn: invalid refresh token")

// dummyHashSalt/dummyHash absorb the same Argon2id cost for unknown-account
// login attempts as a real one incurs, narrowing (not eliminating) the
// timing signal that would otherwise reveal account existence.
var dummyHashSalt, dummyHash = mustHashDummy()

func mustHashDummy() ([]byte, []byte) {
	salt, hash, err := HashPassword("dummy-password-for-timing-parity", DefaultArgon2idParams)
	if err != nil {
		panic(fmt.Sprintf("authn: failed to precompute dummy password hash: %v", err))
	}
	return salt, hash
}

// LoginResult is returned by a successful Login or Refresh.
type LoginResult struct {
	AccessToken     string
	RefreshToken    string
	FamilyID        string
	AccessExpiresAt time.Time
}

// SessionService implements interactive login, refresh rotation, and
// logout, composing the primitives in this package with durable session and
// throttle state.
type SessionService struct {
	db       storage.DB
	users    storage.UserStore
	sessions storage.SessionStore
	throttle storage.ThrottleStore
	audit    *audit.Recorder
	keys     *keys.Material
	issuer   string
	audience string
}

// NewSessionService wires a SessionService. issuer is the canonical
// appliance origin and audience identifies the API audience for session
// JWTs.
func NewSessionService(
	db storage.DB, users storage.UserStore, sessions storage.SessionStore, throttle storage.ThrottleStore,
	recorder *audit.Recorder, keyMaterial *keys.Material, issuer, audience string,
) *SessionService {
	return &SessionService{db: db, users: users, sessions: sessions, throttle: throttle, audit: recorder, keys: keyMaterial, issuer: issuer, audience: audience}
}

// Login authenticates username/password and, on success, creates a new
// session family with an access token and rotating refresh credential.
func (s *SessionService) Login(ctx context.Context, sourceAddr, requestID, username, password string) (LoginResult, error) {
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}

	throttleState, err := s.throttle.Get(ctx, normalized)
	if err != nil {
		return LoginResult{}, err
	}
	now := time.Now().UTC()
	if !throttleState.LockedUntil.IsZero() && now.Before(throttleState.LockedUntil) {
		return LoginResult{}, ErrAccountLocked
	}

	user, err := s.users.GetByUsername(ctx, normalized)
	userExists := err == nil
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return LoginResult{}, err
	}

	var passwordOK bool
	if userExists && user.State == storage.UserStateActive {
		cred, credErr := s.users.GetPasswordCredential(ctx, user.ID)
		if credErr == nil {
			params, paramsErr := DecodeParams(cred.Params)
			if paramsErr == nil {
				passwordOK = VerifyPassword(password, cred.Salt, cred.Hash, params)
			}
		}
	} else {
		// Absorb the same hashing cost as a real verification to narrow
		// the timing signal for account enumeration.
		VerifyPassword(password, dummyHashSalt, dummyHash, DefaultArgon2idParams)
	}

	if !userExists || user.State != storage.UserStateActive || !passwordOK {
		if err := s.recordLoginFailure(ctx, normalized, sourceAddr, requestID); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrInvalidCredentials
	}

	if err := s.throttle.Reset(ctx, normalized); err != nil {
		return LoginResult{}, err
	}

	result, err := s.createSessionFamily(ctx, user)
	if err != nil {
		return LoginResult{}, err
	}

	if err := s.audit.Record(ctx, audit.Actor{UserID: user.ID, Type: storage.AuditActorUser, AuthMethod: "password", RequestID: requestID, SourceAddr: sourceAddr}, audit.Event{
		Action: "auth.login", TargetType: "user", TargetID: user.ID, Outcome: storage.AuditOutcomeSuccess,
	}); err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

func (s *SessionService) recordLoginFailure(ctx context.Context, normalizedUsername, sourceAddr, requestID string) error {
	state, err := s.throttle.RecordFailure(ctx, normalizedUsername, time.Now().UTC(), failureWindowReset, lockDuration, lockThreshold)
	if err != nil {
		return err
	}

	if err := s.audit.Record(ctx, audit.Actor{Type: storage.AuditActorAnonymous, AuthMethod: "password", RequestID: requestID, SourceAddr: sourceAddr}, audit.Event{
		Action: "auth.login", TargetType: "user", TargetID: normalizedUsername, Outcome: storage.AuditOutcomeFailure,
		ReasonCode: "invalid_credentials",
	}); err != nil {
		return err
	}

	if state.FailureCount >= progressiveDelayAt {
		delay := time.Duration(state.FailureCount-progressiveDelayAt+1) * progressiveDelayStep
		if delay > progressiveDelayMax {
			delay = progressiveDelayMax
		}
		time.Sleep(delay)
	}
	return nil
}

func (s *SessionService) createSessionFamily(ctx context.Context, user storage.User) (LoginResult, error) {
	now := time.Now().UTC()
	family := storage.SessionFamily{
		ID: uuid.Must(uuid.NewV7()).String(), UserID: user.ID,
		CreatedAt: now, LastUsedAt: now, AbsoluteExpiresAt: now.Add(RefreshAbsoluteLifetime),
	}

	refreshRaw, refreshSecret, err := GenerateOpaqueCredential()
	if err != nil {
		return LoginResult{}, err
	}
	refresh := storage.RefreshCredential{
		FamilyID: family.ID, CurrentDigest: DigestOpaqueCredential(refreshSecret, s.keys.RefreshPepper),
		Version: 1, ExpiresAt: now.Add(RefreshIdleLifetime), RotatedAt: now,
	}

	var result LoginResult
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		active, err := s.sessions.ListActiveFamiliesForUser(ctx, user.ID)
		if err != nil {
			return err
		}
		if len(active) >= MaxConcurrentFamilies {
			oldest := active[0]
			for _, f := range active {
				if f.LastUsedAt.Before(oldest.LastUsedAt) {
					oldest = f
				}
			}
			if err := s.sessions.RevokeFamily(ctx, oldest.ID, "concurrent session family limit reached"); err != nil {
				return err
			}
		}

		if err := s.sessions.CreateFamily(ctx, family, refresh); err != nil {
			return err
		}

		accessToken, err := s.issueAccessToken(user, family.ID)
		if err != nil {
			return err
		}
		result = LoginResult{
			AccessToken: accessToken, RefreshToken: familyScopedRefreshToken(family.ID, refreshRaw),
			FamilyID: family.ID, AccessExpiresAt: now.Add(SessionAccessLifetime),
		}
		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

func (s *SessionService) issueAccessToken(user storage.User, familyID string) (string, error) {
	now := time.Now().UTC()
	return IssueSessionJWT(s.keys.SessionPrivateKey, s.keys.SessionKeyID, SessionClaims{
		Issuer: s.issuer, Audience: s.audience, Subject: user.ID,
		JTI: uuid.Must(uuid.NewV7()).String(), FamilyID: familyID, CredentialVersion: user.CredentialVersion,
		IssuedAt: now, NotBefore: now, ExpiresAt: now.Add(SessionAccessLifetime),
	})
}

// familyScopedRefreshToken and its parser embed the (non-secret) family ID
// alongside the opaque secret so Refresh can find the right family in O(1)
// time without scanning every active family.
func familyScopedRefreshToken(familyID, opaqueRaw string) string {
	return familyID + "." + opaqueRaw
}

func parseFamilyScopedRefreshToken(token string) (familyID, opaqueRaw string, err error) {
	familyID, opaqueRaw, ok := strings.Cut(token, ".")
	if !ok || familyID == "" || opaqueRaw == "" {
		return "", "", ErrInvalidRefreshToken
	}
	return familyID, opaqueRaw, nil
}

// Refresh rotates a presented refresh credential. Reuse of an
// already-rotated credential revokes the entire family and emits a
// high-severity audit event, per ADR 0010.
func (s *SessionService) Refresh(ctx context.Context, sourceAddr, requestID, refreshToken string) (LoginResult, error) {
	familyID, opaqueRaw, err := parseFamilyScopedRefreshToken(refreshToken)
	if err != nil {
		return LoginResult{}, err
	}
	secret, err := DecodeOpaqueCredential(opaqueRaw)
	if err != nil {
		return LoginResult{}, ErrInvalidRefreshToken
	}

	family, err := s.sessions.GetFamily(ctx, familyID)
	if errors.Is(err, storage.ErrNotFound) {
		return LoginResult{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return LoginResult{}, err
	}
	now := time.Now().UTC()
	if family.RevokedAt != nil || now.After(family.AbsoluteExpiresAt) {
		return LoginResult{}, ErrInvalidRefreshToken
	}

	current, err := s.sessions.GetRefresh(ctx, familyID)
	if err != nil {
		return LoginResult{}, err
	}
	if now.After(current.ExpiresAt) {
		return LoginResult{}, ErrInvalidRefreshToken
	}

	if current.PreviousDigest != nil && VerifyOpaqueCredential(secret, s.keys.RefreshPepper, current.PreviousDigest) {
		// A previously rotated-out credential was replayed: revoke the
		// whole family immediately and flag it for operator attention.
		if err := s.db.WithTx(ctx, func(ctx context.Context) error {
			if err := s.sessions.RevokeFamily(ctx, familyID, "refresh token reuse detected"); err != nil {
				return err
			}
			return s.audit.Record(ctx, audit.Actor{UserID: family.UserID, Type: storage.AuditActorUser, AuthMethod: "refresh_token", RequestID: requestID, SourceAddr: sourceAddr}, audit.Event{
				Action: "auth.refresh_reuse_detected", TargetType: "session_family", TargetID: familyID,
				Outcome: storage.AuditOutcomeDenied, Severity: storage.AuditSeverityHigh,
			})
		}); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrInvalidRefreshToken
	}

	if !VerifyOpaqueCredential(secret, s.keys.RefreshPepper, current.CurrentDigest) {
		return LoginResult{}, ErrInvalidRefreshToken
	}

	user, err := s.users.Get(ctx, family.UserID)
	if err != nil {
		return LoginResult{}, err
	}
	if user.State != storage.UserStateActive {
		return LoginResult{}, ErrInvalidRefreshToken
	}

	newRaw, newSecret, err := GenerateOpaqueCredential()
	if err != nil {
		return LoginResult{}, err
	}
	newDigest := DigestOpaqueCredential(newSecret, s.keys.RefreshPepper)

	var result LoginResult
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.sessions.RotateRefresh(ctx, familyID, newDigest, now.Add(RefreshIdleLifetime)); err != nil {
			return err
		}
		if err := s.sessions.TouchFamily(ctx, familyID, now); err != nil {
			return err
		}
		accessToken, err := s.issueAccessToken(user, familyID)
		if err != nil {
			return err
		}
		result = LoginResult{
			AccessToken: accessToken, RefreshToken: familyScopedRefreshToken(familyID, newRaw),
			FamilyID: familyID, AccessExpiresAt: now.Add(SessionAccessLifetime),
		}
		return s.audit.Record(ctx, audit.Actor{UserID: user.ID, Type: storage.AuditActorUser, AuthMethod: "refresh_token", RequestID: requestID, SourceAddr: sourceAddr}, audit.Event{
			Action: "auth.refresh", TargetType: "session_family", TargetID: familyID, Outcome: storage.AuditOutcomeSuccess,
		})
	})
	if err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

// Logout revokes the current session family only.
func (s *SessionService) Logout(ctx context.Context, actor audit.Actor, familyID string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.sessions.RevokeFamily(ctx, familyID, "logout"); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "auth.logout", TargetType: "session_family", TargetID: familyID, Outcome: storage.AuditOutcomeSuccess,
		})
	})
}

// ValidateAccessToken verifies a presented session access token and checks
// that its embedded credential version still matches the user's current
// one, so a password reset or disable takes effect immediately even though
// the JWT itself hasn't expired yet.
func (s *SessionService) ValidateAccessToken(ctx context.Context, token string) (storage.User, SessionClaims, error) {
	claims, _, err := ValidateSessionJWT(s.keys.SessionPublicKey, s.keys.SessionKeyID, s.issuer, s.audience, token)
	if err != nil {
		return storage.User{}, SessionClaims{}, ErrInvalidCredentials
	}

	user, err := s.users.Get(ctx, claims.Subject)
	if errors.Is(err, storage.ErrNotFound) {
		return storage.User{}, SessionClaims{}, ErrInvalidCredentials
	}
	if err != nil {
		return storage.User{}, SessionClaims{}, err
	}
	if user.State != storage.UserStateActive || user.CredentialVersion != claims.CredentialVersion {
		return storage.User{}, SessionClaims{}, ErrInvalidCredentials
	}
	return user, claims, nil
}
