package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testLimiter struct {
	allow bool
	err   error
}

type failedRevokeStore struct{ *memoryStore }

type failedLookupStore struct{ *memoryStore }

func (failedLookupStore) LookupSession(context.Context, [32]byte, time.Time) (string, error) {
	return "", errors.New("database unavailable")
}

func (failedRevokeStore) RevokeSession(context.Context, [32]byte, time.Time) error {
	return errors.New("database unavailable")
}

func TestLogoutFailureDoesNotClearCookie(t *testing.T) {
	service, err := New(failedRevokeStore{newMemoryStore()}, &testSender{}, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := HTTPHandler{Service: service, Limiter: testLimiter{allow: true}, ClientKey: func(*http.Request) (string, error) { return "client", nil }, AllowedOrigin: "https://app.example"}
	r := httptest.NewRequest(http.MethodPost, "/api/shared/v1/auth/logout", strings.NewReader(`{}`))
	r.Header.Set("Origin", h.AllowedOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: strings.Repeat("a", 43)})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
		t.Fatalf("logout failure response: %d", w.Code)
	}
}

func (l testLimiter) Allow(context.Context, string, string) (bool, error) {
	return l.allow, l.err
}

func TestHTTPChallengeAndSecureSession(t *testing.T) {
	store := newMemoryStore()
	sender := &testSender{}
	service, err := New(store, sender, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := HTTPHandler{Service: service, Limiter: testLimiter{allow: true}, ClientKey: func(*http.Request) (string, error) { return "client", nil }, AllowedOrigin: "https://app.example"}
	request := func(path, body, origin string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	start := httptest.NewRecorder()
	h.ServeHTTP(start, request("/api/shared/v1/auth/otp/challenges", `{"phone":"+919876543210"}`, h.AllowedOrigin))
	var result struct {
		ChallengeID string `json:"challenge_id"`
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(start.Body.Bytes(), &result); err != nil || start.Code != http.StatusCreated || result.ChallengeID == "" {
		t.Fatalf("challenge response: status %d body %s error %v", start.Code, start.Body, err)
	}
	if err := json.Unmarshal(start.Body.Bytes(), &fields); err != nil || len(fields) != 1 || fields["challenge_id"] == nil {
		t.Fatalf("challenge response contains unexpected fields: %s", start.Body)
	}
	verify := httptest.NewRecorder()
	body, err := json.Marshal(map[string]string{"challenge_id": result.ChallengeID, "code": sender.code})
	if err != nil {
		t.Fatal(err)
	}
	h.ServeHTTP(verify, request("/api/shared/v1/auth/otp/verify", string(body), h.AllowedOrigin))
	cookies := verify.Result().Cookies()
	if verify.Code != http.StatusOK || len(cookies) != 1 || cookies[0].Name != SessionCookieName || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" || cookies[0].Domain != "" || cookies[0].SameSite != http.SameSiteLaxMode || verify.Header().Get("Cache-Control") != "no-store" || strings.Contains(verify.Body.String(), cookies[0].Value) {
		t.Fatalf("bad session response: status %d cookies %+v body %s", verify.Code, cookies, verify.Body)
	}
	if id, err := service.UserID(context.Background(), cookies[0].Value); err != nil || id != "user-1" {
		t.Fatalf("session not usable: %q %v", id, err)
	}
	status := func(method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	path := "/api/shared/v1/auth/session"
	if got := status(http.MethodGet, path, nil); got.Code != http.StatusUnauthorized || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("anonymous session status: %d", got.Code)
	}
	if got := status(http.MethodGet, path, cookies[0]); got.Code != http.StatusOK || strings.TrimSpace(got.Body.String()) != `{"authenticated":true}` || len(got.Result().Cookies()) != 0 || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("session status: %d %s", got.Code, got.Body)
	}
	if got := status(http.MethodGet, path+"?extra=1", cookies[0]); got.Code != http.StatusBadRequest {
		t.Fatalf("query: %d", got.Code)
	}
	if got := status(http.MethodPost, path, cookies[0]); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method: %d", got.Code)
	}
	otherChallenge, err := service.Begin(context.Background(), "+919876543211", "client")
	if err != nil {
		t.Fatal(err)
	}
	otherToken, _, err := service.Verify(context.Background(), otherChallenge, sender.code)
	if err != nil {
		t.Fatal(err)
	}
	logout := func(origin string) *httptest.ResponseRecorder {
		r := request("/api/shared/v1/auth/logout", `{}`, origin)
		r.AddCookie(cookies[0])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := logout("https://other.example"); got.Code != http.StatusForbidden {
		t.Fatalf("cross-origin logout: %d", got.Code)
	}
	if _, err := service.UserID(context.Background(), cookies[0].Value); err != nil {
		t.Fatalf("cross-origin request revoked session: %v", err)
	}
	for i := 0; i < 2; i++ {
		got := logout(h.AllowedOrigin)
		cleared := got.Result().Cookies()
		if got.Code != http.StatusOK || len(cleared) != 1 || cleared[0].MaxAge >= 0 || !cleared[0].Secure || !cleared[0].HttpOnly || got.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("logout response: %d %+v", got.Code, cleared)
		}
	}
	if _, err := service.UserID(context.Background(), cookies[0].Value); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked session: %v", err)
	}
	if got := status(http.MethodGet, path, cookies[0]); got.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status: %d", got.Code)
	}
	if _, err := service.UserID(context.Background(), otherToken); err != nil {
		t.Fatalf("other session revoked: %v", err)
	}
	replay := httptest.NewRecorder()
	h.ServeHTTP(replay, request("/api/shared/v1/auth/otp/verify", string(body), h.AllowedOrigin))
	if replay.Code != http.StatusUnauthorized || len(replay.Result().Cookies()) != 0 {
		t.Fatalf("replayed OTP: status %d", replay.Code)
	}
}

func TestSessionStatusDatabaseUnavailable(t *testing.T) {
	service, err := New(failedLookupStore{newMemoryStore()}, &testSender{}, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/shared/v1/auth/session", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: strings.Repeat("a", 43)})
	w := httptest.NewRecorder()
	HTTPHandler{Service: service}.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d", w.Code)
	}
}

func TestHTTPRejectsUntrustedOrUnavailableRequests(t *testing.T) {
	service, err := New(newMemoryStore(), &testSender{}, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	key := func(*http.Request) (string, error) { return "client", nil }
	tests := []struct {
		name    string
		handler HTTPHandler
		origin  string
		body    string
		want    int
	}{
		{name: "foreign origin", handler: HTTPHandler{Service: service, Limiter: testLimiter{allow: true}, ClientKey: key, AllowedOrigin: "https://app.example"}, origin: "https://other.example", body: `{}`, want: 403},
		{name: "rate limited", handler: HTTPHandler{Service: service, Limiter: testLimiter{}, ClientKey: key, AllowedOrigin: "https://app.example"}, origin: "https://app.example", body: `{}`, want: 429},
		{name: "limiter failure", handler: HTTPHandler{Service: service, Limiter: testLimiter{err: errors.New("offline")}, ClientKey: key, AllowedOrigin: "https://app.example"}, origin: "https://app.example", body: `{}`, want: 503},
		{name: "unknown input", handler: HTTPHandler{Service: service, Limiter: testLimiter{allow: true}, ClientKey: key, AllowedOrigin: "https://app.example"}, origin: "https://app.example", body: `{"phone":"+919876543210","admin":true}`, want: 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/shared/v1/auth/otp/challenges", strings.NewReader(tt.body))
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			tt.handler.ServeHTTP(res, r)
			if res.Code != tt.want {
				t.Fatalf("status %d, want %d: %s", res.Code, tt.want, res.Body)
			}
		})
	}
}
