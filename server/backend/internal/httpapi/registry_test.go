package httpapi_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"appliance-code/server/backend/internal/audit"
	"appliance-code/server/backend/internal/roles"
	"appliance-code/server/backend/internal/storage"
)

// issueSelfToken creates an API token for username and returns the raw
// value, for use as the OCI Distribution Basic-auth password.
func (ts *testServer) issueSelfToken(t *testing.T, userID string) string {
	t.Helper()
	raw, _, err := ts.services.Tokens.Create(t.Context(), audit.Actor{Type: storage.AuditActorSystem}, userID, "registry-test-token", 0, nil)
	if err != nil {
		t.Fatalf("creating api token: %v", err)
	}
	return raw
}

func (ts *testServer) registryToken(t *testing.T, username, apiToken string, scopes ...string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/registry/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	q := req.URL.Query()
	q.Set("service", "zot")
	for _, s := range scopes {
		q.Add("scope", s)
	}
	req.URL.RawQuery = q.Encode()
	if apiToken != "" {
		req.SetBasicAuth(username, apiToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/registry/token: %v", err)
	}
	return resp
}

func decodeRegistryToken(t *testing.T, jwt string) map[string]any {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed jwt: %q", jwt)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding jwt payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshalling jwt claims: %v", err)
	}
	return claims
}

func TestRegistryTokenHappyPathAndSignature(t *testing.T) {
	ts := newTestServer(t)
	adminID := ts.bootstrapAdmin(t, "admin", testPassword)
	apiToken := ts.issueSelfToken(t, adminID)

	resp := ts.registryToken(t, "admin", apiToken, "repository:library/nginx:pull,push")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Token     string    `json:"token"`
		ExpiresIn int       `json:"expires_in"`
		IssuedAt  time.Time `json:"issued_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.ExpiresIn != 300 {
		t.Errorf("expires_in = %d, want 300 (5 minutes)", body.ExpiresIn)
	}

	parts := strings.Split(body.Token, ".")
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ts.services.Keys.RegistryPublicKey, []byte(parts[0]+"."+parts[1]), sig) {
		t.Error("registry token should verify against the registry signing public key")
	}

	claims := decodeRegistryToken(t, body.Token)
	access, _ := claims["access"].([]any)
	if len(access) != 1 {
		t.Fatalf("access = %+v, want one entry", access)
	}
	entry, _ := access[0].(map[string]any)
	actions, _ := entry["actions"].([]any)
	if len(actions) != 2 {
		t.Errorf("administrator should be granted pull+push, got %v", actions)
	}
}

func TestRegistryTokenRequiresBasicAuth(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	resp := ts.registryToken(t, "", "", "repository:library/nginx:pull")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status without Basic auth = %d, want 401", resp.StatusCode)
	}
}

func TestRegistryTokenRejectsInvalidCredential(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	resp := ts.registryToken(t, "admin", "apt_totally-bogus-not-a-real-token", "repository:library/nginx:pull")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status with bad credential = %d, want 401", resp.StatusCode)
	}
}

func TestRegistryTokenRejectsMalformedScope(t *testing.T) {
	ts := newTestServer(t)
	adminID := ts.bootstrapAdmin(t, "admin", testPassword)
	apiToken := ts.issueSelfToken(t, adminID)

	resp := ts.registryToken(t, "admin", apiToken, "not-a-valid-scope")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status with malformed scope = %d, want 400", resp.StatusCode)
	}
}

func TestRegistryTokenDeveloperOwnPrefixOnly(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "alice", testPassword, roles.DeveloperRoleID)
	user, err := ts.services.UserStore.GetByUsername(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	apiToken := ts.issueSelfToken(t, user.ID)

	resp := ts.registryToken(t, "alice", apiToken, "repository:users/alice/app:pull,push", "repository:users/bob/app:pull,push")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	claims := decodeRegistryToken(t, body.Token)
	access, _ := claims["access"].([]any)
	if len(access) != 2 {
		t.Fatalf("access = %+v, want two entries", access)
	}
	for _, e := range access {
		entry, _ := e.(map[string]any)
		name, _ := entry["name"].(string)
		actions, _ := entry["actions"].([]any)
		switch name {
		case "users/alice/app":
			if len(actions) != 2 {
				t.Errorf("alice pushing to her own prefix should get pull+push, got %v", actions)
			}
		case "users/bob/app":
			if len(actions) != 1 {
				t.Errorf("alice should only get pull on bob's prefix, got %v", actions)
			}
		default:
			t.Errorf("unexpected access entry name %q", name)
		}
	}
}

func TestRegistryGrantsCRUD(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	adminToken := ts.login(t, "admin", testPassword)

	createBody := `{"subjectType":"user","subjectId":"some-automation-user","pathPrefix":"ci/pipeline-a","actions":["pull","push"]}`
	resp := ts.doJSON(t, "POST", "/api/v1/registry/grants", adminToken, createBody)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	listResp := ts.doJSON(t, "GET", "/api/v1/registry/grants", adminToken, "")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Errorf("list should contain the created grant, got %+v", list.Items)
	}

	delResp := ts.doJSON(t, "DELETE", "/api/v1/registry/grants/"+created.ID, adminToken, "")
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", delResp.StatusCode)
	}
}

func TestRegistryGrantsRequirePermission(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)
	ts.createUserWithRole(t, "viewer-user", testPassword, roles.ViewerRoleID)
	viewerToken := ts.login(t, "viewer-user", testPassword)

	resp := ts.doJSON(t, "GET", "/api/v1/registry/grants", viewerToken, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer listing grants status = %d, want 403", resp.StatusCode)
	}
}
