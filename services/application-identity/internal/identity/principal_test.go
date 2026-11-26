package identity

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type principalTestStore struct{ principal Principal }

func (s *principalTestStore) CreateDiscoveryLink(context.Context, [32]byte, Principal, time.Time) error {
	return nil
}
func (s *principalTestStore) LookupDiscoveryPrincipal(context.Context, [32]byte, time.Time) (Principal, error) {
	return s.principal, nil
}
func (s *principalTestStore) NearbyZones(context.Context, string, float64, float64, float64, int) ([]Zone, error) {
	return nil, nil
}
func (s *principalTestStore) ListActiveMemberships(context.Context, string, string) ([]ActiveMembership, error) {
	return nil, nil
}
func (s *principalTestStore) CreateZoneJoinRequest(context.Context, string, string, string, float64, float64, time.Time) (JoinRequest, error) {
	return JoinRequest{}, nil
}
func (s *principalTestStore) CreateZoneCreationRequest(context.Context, string, string, string, time.Time) (ZoneCreationRequest, error) {
	return ZoneCreationRequest{}, nil
}
func (s *principalTestStore) ResolveChannelSession(_ context.Context, req ChannelSessionRequest, _ [32]byte, sessionID string, _, expires time.Time) (Principal, error) {
	s.principal = Principal{ApplicationUserID: "user-1", TenantID: req.TenantID, ChannelID: req.ChannelID, Audience: CustomerAudience, SubjectID: "subject-1", SessionID: sessionID, AuthMethod: "channel_proof", ExpiresAt: expires, Permissions: []string{}}
	return s.principal, nil
}

func TestInternalPrincipalResolutionRequiresTargetKey(t *testing.T) {
	store := &principalTestStore{principal: Principal{ApplicationUserID: "user-1"}}
	service := NewOnboardingService(store, "https://app.example", nil)
	key := []byte(strings.Repeat("i", 32))
	h := HTTPHandler{Service: service, InternalKey: key}
	request := func(header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/internal/v1/application-principal/resolve", strings.NewReader(`{"channel_id":"customer-web","subject_id":"opaque","phone":"+919876543210"}`))
		r.Header.Set("Content-Type", "application/json")
		if header != "" {
			r.Header.Set("X-Application-Identity-Key", header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := request(""); got.Code != http.StatusUnauthorized {
		t.Fatalf("missing key status = %d", got.Code)
	}
	if got := request(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key status = %d", got.Code)
	}
	got := request(base64.StdEncoding.EncodeToString(key))
	if got.Code != http.StatusCreated || !strings.Contains(got.Body.String(), `"channel_id":"customer-web"`) || strings.Contains(got.Body.String(), "+919876543210") {
		t.Fatalf("principal resolution status = %d body = %s", got.Code, got.Body)
	}
}

func TestChannelPrincipalRequiresAuthResolverForWebSessions(t *testing.T) {
	store := &principalTestStore{}
	service := NewOnboardingService(store, "https://app.example", nil)
	if _, err := service.Principal(context.Background(), base64.RawURLEncoding.EncodeToString(make([]byte, 32))); err == nil {
		t.Fatal("web session resolved without auth service")
	}
}
