package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultTenantID       = "zonited-solutions"
	CustomerWebChannelID  = "customer-web"
	CustomerAudience      = "customer"
	ChannelSubjectLinked  = "linked"
	ChannelSubjectPending = "pending"
	ChannelSubjectBlocked = "blocked"
)

var (
	ErrUnknownChannel   = errors.New("unknown or disabled channel")
	ErrSubjectBlocked   = errors.New("channel subject is blocked")
	ErrSubjectNotLinked = errors.New("channel subject is not linked")
	channelIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9.-]{1,62}$`)
	tenantIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9.-]{1,62}$`)
)

// Principal is the provider-neutral application identity consumed by later
// gateways and application services. It deliberately contains no provider
// identifier, phone number, or UI route.
type Principal struct {
	ApplicationUserID string    `json:"application_user_id"`
	TenantID          string    `json:"tenant_id"`
	ChannelID         string    `json:"channel_id"`
	Audience          string    `json:"audience"`
	SubjectID         string    `json:"subject_id,omitempty"`
	SessionID         string    `json:"session_id,omitempty"`
	AuthMethod        string    `json:"auth_method"`
	ExpiresAt         time.Time `json:"expires_at"`
	Roles             []string  `json:"roles"`
	Permissions       []string  `json:"permissions"`
	ZoneScopes        []string  `json:"zone_scopes"`
	DiscoveryURL      string    `json:"discovery_url,omitempty"`
}

// ChannelSessionRequest is the generic input accepted from a trusted channel
// adapter. The adapter must validate its provider proof and map the provider
// sender to an opaque subject ID before calling this API.
type ChannelSessionRequest struct {
	TenantID  string `json:"tenant_id"`
	ChannelID string `json:"channel_id"`
	SubjectID string `json:"subject_id"`
	Phone     string `json:"phone,omitempty"`
}

// PrincipalStore is implemented by the durable identity store. It is kept
// separate from Store so the existing web OTP tests and adapter contract stay
// source-compatible while the generic principal contract is introduced.
type PrincipalStore interface {
	LookupPrincipal(context.Context, [32]byte, time.Time) (Principal, error)
	ResolveChannelSession(context.Context, ChannelSessionRequest, [32]byte, string, time.Time, time.Time) (Principal, error)
}

// Principal resolves a valid application session without exposing its token.
func (s *Service) Principal(ctx context.Context, token string) (Principal, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return Principal{}, ErrUnauthenticated
	}
	store, ok := s.store.(PrincipalStore)
	if !ok {
		return Principal{}, errors.New("principal lookup is unavailable")
	}
	principal, err := store.LookupPrincipal(ctx, sha256.Sum256([]byte(token)), s.now())
	if err != nil {
		return Principal{}, err
	}
	if principal.ApplicationUserID == "" {
		return Principal{}, ErrUnauthenticated
	}
	return principal, nil
}

// ResolveChannelSession links a validated channel subject to an application
// user according to the channel's server-side policy and creates a short-lived
// opaque session for the interaction. It never accepts provider-specific data.
func (s *Service) ResolveChannelSession(ctx context.Context, request ChannelSessionRequest) (string, Principal, error) {
	request.TenantID = strings.TrimSpace(request.TenantID)
	if request.TenantID == "" {
		request.TenantID = DefaultTenantID
	}
	request.ChannelID = strings.TrimSpace(request.ChannelID)
	request.SubjectID = strings.TrimSpace(request.SubjectID)
	request.Phone = strings.TrimSpace(request.Phone)
	if !tenantIDPattern.MatchString(request.TenantID) || !channelIDPattern.MatchString(request.ChannelID) || request.SubjectID == "" || len(request.SubjectID) > 256 {
		return "", Principal{}, ErrSubjectNotLinked
	}
	if request.Phone != "" && !phonePattern.MatchString(request.Phone) {
		return "", Principal{}, ErrInvalidPhone
	}
	store, ok := s.store.(PrincipalStore)
	if !ok {
		return "", Principal{}, errors.New("principal resolution is unavailable")
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", Principal{}, fmt.Errorf("generate channel session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	sessionIDBytes := make([]byte, 16)
	if _, err := rand.Read(sessionIDBytes); err != nil {
		return "", Principal{}, fmt.Errorf("generate channel session id: %w", err)
	}
	now := s.now()
	expires := now.Add(s.channelSessionTTL)
	principal, err := store.ResolveChannelSession(ctx, request, sha256.Sum256([]byte(token)), hex.EncodeToString(sessionIDBytes), now, expires)
	if err != nil {
		return "", Principal{}, err
	}
	return token, principal, nil
}
