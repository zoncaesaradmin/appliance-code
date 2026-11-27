package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

// PrincipalStore is implemented by the durable identity store for channel
// subject linking. Browser session validation is delegated to Auth Service.
type PrincipalStore interface {
	ResolveChannelSession(context.Context, ChannelSessionRequest, [32]byte, string, time.Time, time.Time) (Principal, error)
}

// PrincipalResolver is the small auth boundary used by onboarding. Keeping
// it separate from the domain store lets Auth Service live in another
// namespace, cluster, or EC2 instance; the current schema still requires the
// Auth and domain stores to use the same durable PostgreSQL data source.
type PrincipalResolver interface {
	ResolvePrincipal(context.Context, string) (Principal, error)
}

type HTTPPrincipalResolver struct {
	URL    string
	Client *http.Client
}

func (c HTTPPrincipalResolver) ResolvePrincipal(ctx context.Context, token string) (Principal, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.URL, "/")+"/internal/v1/auth/principal", nil)
	if err != nil {
		return Principal{}, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return Principal{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return Principal{}, ErrUnauthenticated
	}
	if response.StatusCode/100 != 2 {
		return Principal{}, fmt.Errorf("auth principal lookup returned status %d", response.StatusCode)
	}
	var principal Principal
	if err := json.NewDecoder(response.Body).Decode(&principal); err != nil {
		return Principal{}, err
	}
	if principal.ApplicationUserID == "" {
		return Principal{}, ErrUnauthenticated
	}
	return principal, nil
}

// Principal resolves a valid application session without exposing its token.
func (s *Service) Principal(ctx context.Context, token string) (Principal, error) {
	if s.principalResolver == nil {
		return Principal{}, errors.New("auth service principal resolver is unavailable")
	}
	return s.principalResolver.ResolvePrincipal(ctx, token)
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
	store := s.store
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
