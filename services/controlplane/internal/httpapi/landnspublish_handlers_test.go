package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/roles"
)

func TestLANDNSPublish_ProxiesToRemoteDNSAppliance(t *testing.T) {
	var gotAuth, gotPath string
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"registry1"}`))
	}))
	t.Cleanup(remote.Close)

	ts := newTestServerWithProfile(t, appliance.ProfileCore)
	_ = ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	body := map[string]any{
		"dnsApplianceURL": remote.URL,
		"apiToken":        "remote-tok",
		"name":            "registry1",
		"ipv4":            "192.0.2.20",
		"ttl":             60,
	}
	payload, _ := json.Marshal(body)
	resp := ts.doJSON(t, http.MethodPost, "/api/v1/dns/publish", token, string(payload))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	if gotAuth != "Bearer remote-tok" {
		t.Fatalf("remote Authorization = %q", gotAuth)
	}
	if gotPath != "/api/v1/dns/records/registry1" {
		t.Fatalf("remote path = %q", gotPath)
	}
}

func TestLANDNSPublish_RequiresPermission(t *testing.T) {
	ts := newTestServerWithProfile(t, appliance.ProfileCore)
	_ = ts.bootstrapAdmin(t, "admin", testPassword)
	_ = ts.createUserWithRole(t, "viewer", testPassword, roles.ViewerRoleID)
	token := ts.login(t, "viewer", testPassword)

	resp := ts.doJSON(t, http.MethodPost, "/api/v1/dns/publish", token, `{
		"dnsApplianceURL":"https://dns.example",
		"apiToken":"tok",
		"name":"registry1",
		"ipv4":"192.0.2.20"
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
}
