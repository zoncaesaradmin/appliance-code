// Package webui owns one-time appliance-to-WebUI browser handoffs.
package webui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"appliance-code/services/controlplane/internal/authn"
	"appliance-code/services/controlplane/internal/storage"
)

const GrantLifetime = 60 * time.Second

type Service struct {
	store  storage.WebUILaunchGrantStore
	pepper []byte
	now    func() time.Time
}

func New(store storage.WebUILaunchGrantStore, pepper []byte) (*Service, error) {
	if store == nil || len(pepper) < 16 {
		return nil, fmt.Errorf("webui: store and grant pepper are required")
	}
	return &Service{store: store, pepper: pepper, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Issue is session-only by construction: callers supply a FamilyID obtained
// from reqauth.Principal after rejecting API-token authentication.
func (s *Service) Issue(ctx context.Context, userID, familyID string) (string, error) {
	if userID == "" || familyID == "" {
		return "", fmt.Errorf("webui: user and session family are required")
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
