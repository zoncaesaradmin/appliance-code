package identity

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type proxyStore struct{}

func (proxyStore) CreateDiscoveryLink(context.Context, [32]byte, Principal, time.Time) error {
	return nil
}

func (proxyStore) LookupDiscoveryPrincipal(context.Context, [32]byte, time.Time) (Principal, error) {
	return Principal{}, nil
}

func (proxyStore) NearbyZones(context.Context, string, float64, float64, float64, int) ([]Zone, error) {
	return nil, nil
}

func (proxyStore) ListActiveMemberships(context.Context, string, string) ([]ActiveMembership, error) {
	return nil, nil
}

func (proxyStore) CreateZoneJoinRequest(context.Context, string, string, string, float64, float64, time.Time) (JoinRequest, error) {
	return JoinRequest{}, nil
}

func (proxyStore) CreateZoneCreationRequest(context.Context, string, string, string, time.Time) (ZoneCreationRequest, error) {
	return ZoneCreationRequest{}, nil
}

func (proxyStore) ResolveChannelSession(context.Context, ChannelSessionRequest, [32]byte, string, time.Time, time.Time) (Principal, error) {
	return Principal{}, nil
}

func TestAuthRoutesProxyToConfiguredService(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/app/v1/auth/otp/verify" || r.Header.Get("Origin") != "https://app.example" {
			t.Fatalf("unexpected forwarded request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Add("Set-Cookie", "__Host-app_session=test; Path=/; Secure; HttpOnly")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	service := NewOnboardingService(proxyStore{}, "https://app.example", nil)
	h := HTTPHandler{Service: service, AuthServiceURL: upstream.URL, AuthClient: upstream.Client()}
	r := httptest.NewRequest(http.MethodPost, "/api/app/v1/auth/otp/verify?x=1", strings.NewReader(`{"challenge_id":"id","code":"123456"}`))
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != `{"ok":true}` || len(w.Result().Cookies()) != 1 {
		t.Fatalf("proxy response: %d %s cookies=%d", w.Code, w.Body.String(), len(w.Result().Cookies()))
	}
}

func TestAuthRoutesFailClosedWithoutEndpoint(t *testing.T) {
	h := HTTPHandler{}
	r := httptest.NewRequest(http.MethodPost, "/api/app/v1/auth/otp/challenges", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", w.Code)
	}
}

func TestAuthCapabilitiesProxyToConfiguredService(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/app/v1/auth/capabilities" || r.Method != http.MethodGet {
			t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"authentication":{"methods":["otp"]}}`)
	}))
	defer upstream.Close()
	h := HTTPHandler{AuthServiceURL: upstream.URL, AuthClient: upstream.Client()}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/app/v1/auth/capabilities", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"methods"`) {
		t.Fatalf("unexpected proxied response: %d %s", w.Code, w.Body.String())
	}
}
