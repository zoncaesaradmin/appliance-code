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

type principalTestStore struct {
	*memoryStore
	principal Principal
}

func (s *principalTestStore) LookupPrincipal(_ context.Context, _ [32]byte, _ time.Time) (Principal, error) {
	return s.principal, nil
}

func (s *principalTestStore) ResolveChannelSession(_ context.Context, req ChannelSessionRequest, _ [32]byte, sessionID string, _, expires time.Time) (Principal, error) {
	s.principal = Principal{
		ApplicationUserID: "user-1",
		TenantID:          req.TenantID,
		ChannelID:         req.ChannelID,
		Audience:          CustomerAudience,
		SubjectID:         "subject-1",
		SessionID:         sessionID,
		AuthMethod:        "channel_proof",
		ExpiresAt:         expires,
		Permissions:       []string{},
	}
	return s.principal, nil
}

func TestInternalPrincipalResolutionRequiresTargetKey(t *testing.T) {
	store := &principalTestStore{memoryStore: newMemoryStore()}
	store.principal = Principal{ApplicationUserID: "user-1"}
	service, err := New(store, &testSender{}, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
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

func TestPrincipalRejectsInvalidTenantAndInvalidSession(t *testing.T) {
	store := &principalTestStore{memoryStore: newMemoryStore()}
	store.principal = Principal{ApplicationUserID: "user-1"}
	service, err := New(store, &testSender{}, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ResolveChannelSession(context.Background(), ChannelSessionRequest{TenantID: "not valid", ChannelID: CustomerWebChannelID, SubjectID: "subject"}); err == nil {
		t.Fatal("invalid tenant accepted")
	}
	if _, err := service.Principal(context.Background(), "not-a-token"); err != ErrUnauthenticated {
		t.Fatalf("invalid session error = %v", err)
	}
	if _, err := service.Principal(context.Background(), base64.RawURLEncoding.EncodeToString(make([]byte, 32))); err != nil {
		// The test store intentionally returns a principal for any valid hash;
		// this assertion makes sure the token shape is accepted before lookup.
		t.Fatalf("valid-shaped session was not looked up: %v", err)
	}
}
