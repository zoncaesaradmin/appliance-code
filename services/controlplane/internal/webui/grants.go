// Package webui owns one-time appliance-to-WebUI browser handoffs.
package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/authz"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

const (
	GrantLifetime  = 60 * time.Second
	BridgeLifetime = 8 * time.Hour
)

type Service struct {
	store    storage.WebUILaunchGrantStore
	bridges  storage.WebUIBridgeSessionStore
	users    storage.UserStore
	sessions storage.SessionStore
	authz    *authz.Service
	pepper   []byte
	now      func() time.Time
}

func New(store storage.WebUILaunchGrantStore, bridges storage.WebUIBridgeSessionStore, users storage.UserStore, sessions storage.SessionStore, authorizer *authz.Service, pepper []byte) (*Service, error) {
	if store == nil || bridges == nil || users == nil || sessions == nil || authorizer == nil || len(pepper) < 16 {
		return nil, fmt.Errorf("webui: stores, authorizer, and grant pepper are required")
	}
	return &Service{store: store, bridges: bridges, users: users, sessions: sessions, authz: authorizer, pepper: pepper, now: func() time.Time { return time.Now().UTC() }}, nil
}

// ConsumeGrant atomically burns a launch grant and creates an opaque bridge
// session. The returned raw value is only ever placed in the gateway cookie.
func (s *Service) ConsumeGrant(ctx context.Context, raw string) (bridge, userID, displayName string, err error) {
	if len(raw) < 17 {
		return "", "", "", storage.ErrNotFound
	}
	g, err := s.store.GetWebUILaunchGrantByLookupID(ctx, raw[:16])
	if err != nil || g.UsedAt != nil || !g.ExpiresAt.After(s.now()) || subtle.ConstantTimeCompare(g.Digest, authn.DigestOpaqueCredential([]byte(raw), s.pepper)) != 1 {
		return "", "", "", storage.ErrNotFound
	}
	user, family, err := s.validateActor(ctx, g.UserID, g.FamilyID)
	if err != nil {
		return "", "", "", storage.ErrNotFound
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", err
	}
	bridge = base64.RawURLEncoding.EncodeToString(b)
	now := s.now()
	session := storage.WebUIBridgeSession{ID: bridge[16:], LookupID: bridge[:16], Digest: authn.DigestOpaqueCredential([]byte(bridge), s.pepper), UserID: user.ID, FamilyID: family.ID, CreatedAt: now, ExpiresAt: now.Add(BridgeLifetime)}
	if err := s.store.ConsumeWebUILaunchGrant(ctx, g.ID, now); err != nil {
		return "", "", "", err
	}
	if err := s.bridges.CreateWebUIBridgeSession(ctx, session); err != nil {
		return "", "", "", err
	}
	return bridge, user.ID, user.DisplayName, nil
}

// ValidateBridge rechecks all revocable state on every gateway request.
func (s *Service) ValidateBridge(ctx context.Context, raw string) (userID, displayName string, err error) {
	if len(raw) < 17 {
		return "", "", storage.ErrNotFound
	}
	b, err := s.bridges.GetWebUIBridgeSessionByLookupID(ctx, raw[:16])
	if err != nil || b.RevokedAt != nil || !b.ExpiresAt.After(s.now()) || subtle.ConstantTimeCompare(b.Digest, authn.DigestOpaqueCredential([]byte(raw), s.pepper)) != 1 {
		return "", "", storage.ErrNotFound
	}
	user, _, err := s.validateActor(ctx, b.UserID, b.FamilyID)
	if err != nil {
		return "", "", storage.ErrNotFound
	}
	return user.ID, user.DisplayName, nil
}

// Issue is session-only by construction: callers supply a FamilyID obtained
// from reqauth.Principal after rejecting API-token authentication.
func (s *Service) Issue(ctx context.Context, userID, familyID string) (string, error) {
	if userID == "" || familyID == "" {
		return "", fmt.Errorf("webui: user and session family are required")
	}
	if _, _, err := s.validateActor(ctx, userID, familyID); err != nil {
		return "", storage.ErrNotFound
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	raw := base64.RawURLEncoding.EncodeToString(b)
	lookup := raw[:16]
	now := s.now()
	g := storage.WebUILaunchGrant{ID: raw[16:], LookupID: lookup, Digest: authn.DigestOpaqueCredential([]byte(raw), s.pepper), UserID: userID, FamilyID: familyID, CreatedAt: now, ExpiresAt: now.Add(GrantLifetime)}
	if e := s.store.CreateWebUILaunchGrant(ctx, g); e != nil {
		return "", e
	}
	return raw, nil
}

// validateActor is shared by issuance, grant consumption, and bridge
// validation so a grant can never outlive a disabled user, revoked session
// family, or removed inference permission.
func (s *Service) validateActor(ctx context.Context, userID, familyID string) (storage.User, storage.SessionFamily, error) {
	user, err := s.users.Get(ctx, userID)
	if err != nil || user.State != storage.UserStateActive {
		return storage.User{}, storage.SessionFamily{}, storage.ErrNotFound
	}
	family, err := s.sessions.GetFamily(ctx, familyID)
	if err != nil || family.UserID != user.ID || family.RevokedAt != nil || !family.AbsoluteExpiresAt.After(s.now()) {
		return storage.User{}, storage.SessionFamily{}, storage.ErrNotFound
	}
	if err := s.authz.Check(ctx, user.ID, roles.PermInferenceUse); err != nil {
		return storage.User{}, storage.SessionFamily{}, storage.ErrNotFound
	}
	return user, family, nil
}
