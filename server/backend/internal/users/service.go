// Package users owns local-user business logic above storage.UserStore:
// creation, profile updates, disable/enable, and administrator-initiated
// password reset, including the invariants that keep the appliance from
// ever losing its last enabled administrator.
package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"appliance-code/server/backend/internal/audit"
	"appliance-code/server/backend/internal/authn"
	"appliance-code/server/backend/internal/keys"
	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/storage"
)

// ErrLastAdministrator is returned when an operation would leave the
// appliance without an enabled effective administrator.
var ErrLastAdministrator = errors.New("users: cannot remove the last enabled administrator")

// PasswordResetTTL is the fixed ADR 0010 lifetime for an
// administrator-issued password-reset credential.
const PasswordResetTTL = 15 * time.Minute

// Service implements the user lifecycle business logic described above.
type Service struct {
	db       storage.DB
	users    storage.UserStore
	roles    storage.RoleStore
	tokens   storage.TokenStore
	sessions storage.SessionStore
	throttle storage.ThrottleStore
	audit    *audit.Recorder
	keys     *keys.Material
}

// NewService wires a Service from its storage and shared-secret
// dependencies.
func NewService(
	db storage.DB,
	users storage.UserStore,
	roles storage.RoleStore,
	tokens storage.TokenStore,
	sessions storage.SessionStore,
	throttle storage.ThrottleStore,
	recorder *audit.Recorder,
	keyMaterial *keys.Material,
) *Service {
	return &Service{db: db, users: users, roles: roles, tokens: tokens, sessions: sessions, throttle: throttle, audit: recorder, keys: keyMaterial}
}

// Create validates and creates a new local user with a password credential.
// It does not assign any role; callers assign roles separately (bootstrap
// assigns administrator directly).
func (s *Service) Create(ctx context.Context, actor audit.Actor, username, displayName, password string) (storage.User, error) {
	normalized, err := authn.NormalizeUsername(username)
	if err != nil {
		return storage.User{}, err
	}
	if err := authn.ValidatePasswordPolicy(password); err != nil {
		return storage.User{}, err
	}

	salt, hash, err := authn.HashPassword(password, authn.DefaultArgon2idParams)
	if err != nil {
		return storage.User{}, err
	}
	paramsJSON, err := authn.EncodeParams(authn.DefaultArgon2idParams)
	if err != nil {
		return storage.User{}, err
	}

	now := time.Now().UTC()
	user := storage.User{
		ID: uuid.Must(uuid.NewV7()).String(), Username: normalized, DisplayName: displayName,
		State: storage.UserStateActive, CredentialVersion: 1, CreatedAt: now, UpdatedAt: now,
	}

	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.users.Create(ctx, user); err != nil {
			return err
		}
		if err := s.users.SetPassword(ctx, storage.PasswordCredential{
			UserID: user.ID, Algorithm: authn.PasswordAlgorithmArgon2id, Params: paramsJSON, Salt: salt, Hash: hash, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "users.create", TargetType: "user", TargetID: user.ID, Outcome: storage.AuditOutcomeSuccess,
			Details: map[string]any{"username": normalized},
		})
	})
	if errors.Is(err, storage.ErrConflict) {
		return storage.User{}, fmt.Errorf("users: username %q is already in use", normalized)
	}
	if err != nil {
		return storage.User{}, err
	}
	return user, nil
}

func (s *Service) Get(ctx context.Context, id string) (storage.User, error) {
	return s.users.Get(ctx, id)
}

func (s *Service) GetByUsername(ctx context.Context, username string) (storage.User, error) {
	return s.users.GetByUsername(ctx, username)
}

func (s *Service) List(ctx context.Context) ([]storage.User, error) {
	return s.users.List(ctx)
}

func (s *Service) UpdateDisplayName(ctx context.Context, actor audit.Actor, id, displayName string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.users.UpdateDisplayName(ctx, id, displayName); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "users.update", TargetType: "user", TargetID: id, Outcome: storage.AuditOutcomeSuccess,
		})
	})
}

// wouldRemoveLastAdministrator reports whether disabling/demoting userID
// would leave zero enabled administrators.
func (s *Service) wouldRemoveLastAdministrator(ctx context.Context, userID string) (bool, error) {
	userRoles, err := s.roles.ListUserRoles(ctx, userID)
	if err != nil {
		return false, err
	}
	isAdmin := false
	for _, r := range userRoles {
		if r.ID == roles.AdministratorRoleID {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		return false, nil
	}
	count, err := s.users.CountEnabledAdministrators(ctx, roles.AdministratorRoleID)
	if err != nil {
		return false, err
	}
	return count <= 1, nil
}

// Disable soft-disables a user, immediately revoking every interactive
// session and API token they own, and refuses to leave the appliance
// without an enabled effective administrator.
func (s *Service) Disable(ctx context.Context, actor audit.Actor, id string) error {
	if blocked, err := s.wouldRemoveLastAdministrator(ctx, id); err != nil {
		return err
	} else if blocked {
		return ErrLastAdministrator
	}

	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.users.SetState(ctx, id, storage.UserStateDisabled); err != nil {
			return err
		}
		if err := s.users.BumpCredentialVersion(ctx, id); err != nil {
			return err
		}
		if err := s.sessions.RevokeAllFamiliesForUser(ctx, id, "user disabled"); err != nil {
			return err
		}
		if err := s.tokens.RevokeAllForUser(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "users.disable", TargetType: "user", TargetID: id, Outcome: storage.AuditOutcomeSuccess,
		})
	})
}

// Enable re-enables a previously disabled user. It does not restore any
// revoked session or API token.
func (s *Service) Enable(ctx context.Context, actor audit.Actor, id string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.users.SetState(ctx, id, storage.UserStateActive); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "users.enable", TargetType: "user", TargetID: id, Outcome: storage.AuditOutcomeSuccess,
		})
	})
}

// InitiatePasswordReset issues a single-use, 15-minute password-reset
// credential for id, returned once as raw. Administrators never choose or
// view the user's new password.
func (s *Service) InitiatePasswordReset(ctx context.Context, actor audit.Actor, id string) (raw string, err error) {
	raw, lookupID, secret, err := authn.GenerateResetCredential()
	if err != nil {
		return "", err
	}
	digest := authn.DigestOpaqueCredential(secret, s.keys.RefreshPepper)

	now := time.Now().UTC()
	cred := storage.PasswordResetCredential{
		ID: uuid.Must(uuid.NewV7()).String(), UserID: id, LookupID: lookupID, Digest: digest,
		CreatedAt: now, ExpiresAt: now.Add(PasswordResetTTL),
	}

	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.users.CreatePasswordReset(ctx, cred); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "users.password_reset.initiate", TargetType: "user", TargetID: id, Outcome: storage.AuditOutcomeSuccess,
		})
	})
	if err != nil {
		return "", err
	}
	return raw, nil
}

// ErrInvalidResetCredential covers every way a presented reset credential
// can fail: not found, expired, already used, or a digest mismatch. The
// specific cause is deliberately not distinguished to callers.
var ErrInvalidResetCredential = errors.New("users: invalid or expired reset credential")

// CompletePasswordReset validates raw against a previously issued
// credential and, if valid, sets newPassword, revokes every session and API
// token the user owns, and marks the credential used.
func (s *Service) CompletePasswordReset(ctx context.Context, raw, newPassword string) error {
	lookupID, secret, err := authn.ParseResetCredential(raw)
	if err != nil {
		return ErrInvalidResetCredential
	}
	cred, err := s.users.GetPasswordResetByLookupID(ctx, lookupID)
	if errors.Is(err, storage.ErrNotFound) {
		return ErrInvalidResetCredential
	}
	if err != nil {
		return err
	}
	if cred.UsedAt != nil || time.Now().UTC().After(cred.ExpiresAt) {
		return ErrInvalidResetCredential
	}
	if !authn.VerifyOpaqueCredential(secret, s.keys.RefreshPepper, cred.Digest) {
		return ErrInvalidResetCredential
	}

	if err := s.db.WithTx(ctx, func(ctx context.Context) error {
		return s.users.MarkPasswordResetUsed(ctx, cred.ID)
	}); err != nil {
		return err
	}

	return s.setPasswordAndRevoke(ctx, audit.Actor{UserID: cred.UserID, Type: storage.AuditActorUser, AuthMethod: "password_reset"}, cred.UserID, newPassword, "users.password_reset.complete")
}

// SetPasswordDirect sets userID's password without a reset-credential
// round trip, for the node-local break-glass recovery command. It applies
// the same policy validation and session/token revocation as
// CompletePasswordReset.
func (s *Service) SetPasswordDirect(ctx context.Context, actor audit.Actor, userID, newPassword string) error {
	return s.setPasswordAndRevoke(ctx, actor, userID, newPassword, "users.password_reset.break_glass")
}

// setPasswordAndRevoke validates newPassword, sets it, bumps the user's
// credential version, and revokes every session and API token they own, all
// in one transaction, then records a high-severity audit event under
// action.
func (s *Service) setPasswordAndRevoke(ctx context.Context, actor audit.Actor, userID, newPassword, action string) error {
	if err := authn.ValidatePasswordPolicy(newPassword); err != nil {
		return err
	}
	salt, hash, err := authn.HashPassword(newPassword, authn.DefaultArgon2idParams)
	if err != nil {
		return err
	}
	paramsJSON, err := authn.EncodeParams(authn.DefaultArgon2idParams)
	if err != nil {
		return err
	}

	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.users.SetPassword(ctx, storage.PasswordCredential{
			UserID: userID, Algorithm: authn.PasswordAlgorithmArgon2id, Params: paramsJSON, Salt: salt, Hash: hash,
		}); err != nil {
			return err
		}
		if err := s.users.BumpCredentialVersion(ctx, userID); err != nil {
			return err
		}
		if err := s.sessions.RevokeAllFamiliesForUser(ctx, userID, "password reset"); err != nil {
			return err
		}
		if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: action, TargetType: "user", TargetID: userID,
			Outcome: storage.AuditOutcomeSuccess, Severity: storage.AuditSeverityHigh,
		})
	})
}

// Unlock clears any durable login-throttle lockout for username, used by
// the node-local break-glass recovery command.
func (s *Service) Unlock(ctx context.Context, actor audit.Actor, username string) error {
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.throttle.Reset(ctx, username); err != nil {
			return err
		}
		return s.audit.Record(ctx, actor, audit.Event{
			Action: "users.unlock", TargetType: "user", TargetID: username, Outcome: storage.AuditOutcomeSuccess,
		})
	})
}
