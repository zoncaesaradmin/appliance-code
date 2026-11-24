package identityclient

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveUsesGenericContractAndDoesNotExposeProviderData(t *testing.T) {
	key := []byte(strings.Repeat("i", 32))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/application-principal/resolve" || r.Header.Get("X-Application-Identity-Key") != base64.StdEncoding.EncodeToString(key) {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"principal":{"application_user_id":"user-1","tenant_id":"zonited-solutions","channel_id":"customer-whatsapp","audience":"customer","auth_method":"channel_proof","permissions":[]}}`))
	}))
	defer server.Close()
	client, err := New(server.URL, base64.StdEncoding.EncodeToString(key), "zonited-solutions", "customer-whatsapp")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := client.Resolve(context.Background(), "919999999999", "+919999999999")
	if err != nil || principal.ApplicationUserID != "user-1" || principal.ChannelID != "customer-whatsapp" {
		t.Fatalf("principal = %+v, error = %v", principal, err)
	}
}

func TestNewRejectsWeakKey(t *testing.T) {
	if _, err := New("http://identity", base64.StdEncoding.EncodeToString([]byte("short")), "zonited-solutions", "customer-whatsapp"); err == nil {
		t.Fatal("weak key accepted")
	}
}
