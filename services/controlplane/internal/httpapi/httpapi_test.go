package httpapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"appliance-code/services/controlplane/internal/app"
	"appliance-code/services/controlplane/internal/audit"
	"appliance-code/services/controlplane/internal/bootstrap"
	"appliance-code/services/controlplane/internal/config"
	"appliance-code/services/controlplane/internal/httpapi"
	"appliance-code/services/controlplane/internal/logging"
	"appliance-code/services/controlplane/internal/roles"
	"appliance-code/services/controlplane/internal/storage"
)

type testServer struct {
	*httptest.Server
	services *app.Services
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AllowedGitSourceHosts = []string{"git.internal.example.com"}
	cfg.AllowedBuilderImageDigests = []string{"buildah@sha256:approved"}

	services, err := app.WireServices(cfg)
	if err != nil {
		t.Fatalf("WireServices: %v", err)
	}
	t.Cleanup(func() { services.DB.Close() })

	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	authDeps := httpapi.AuthDeps{Sessions: services.Sessions, Tokens: services.Tokens, Authz: services.Authz}
	deps := httpapi.Deps{
		Logger:  logger,
		Auth:    authDeps,
		AuthH:   &httpapi.AuthHandlers{Sessions: services.Sessions},
		UsersH:  &httpapi.UserHandlers{Users: services.Users, Roles: services.Roles},
		RolesH:  &httpapi.RoleHandlers{Roles: services.Roles},
		TokensH: &httpapi.TokenHandlers{Tokens: services.Tokens},
		RegistryH: &httpapi.RegistryTokenHandlers{
			Auth: authDeps, Users: services.Users, Authorizer: services.RegistryAuthorizer,
			Keys: services.Keys, Issuer: cfg.CanonicalOrigin,
		},
		RegistryGrantsH: &httpapi.RegistryGrantHandlers{Grants: services.RegistryGrantStore},
		RegistryCatalogH: &httpapi.RegistryCatalogHandlers{
			Zot: services.Zot, Authorizer: services.RegistryAuthorizer, Users: services.Users,
		},
		BuildsH: &httpapi.BuildHandlers{Builds: services.Builds},
	}

	srv := httptest.NewServer(httpapi.NewPublicMux(deps))
	t.Cleanup(srv.Close)
	return &testServer{Server: srv, services: services}
}

// bootstrapAdmin creates the first administrator directly through the
// bootstrap package, mirroring how the real CLI does it.
func (ts *testServer) bootstrapAdmin(t *testing.T, username, password string) string {
	t.Helper()
	result, err := bootstrap.Init(t.Context(), ts.services.DB, ts.services.UserStore, ts.services.RoleStore, ts.services.Users, username, password, username)
	if err != nil {
		t.Fatalf("bootstrap.Init: %v", err)
	}
	return result.AdminUserID
}

// createUserWithRole creates a user assigned exactly roleID, for driving
// the RBAC probe matrix.
func (ts *testServer) createUserWithRole(t *testing.T, username, password, roleID string) string {
	t.Helper()
	actor := audit.Actor{Type: storage.AuditActorSystem, AuthMethod: "test"}
	user, err := ts.services.Users.Create(t.Context(), actor, username, username, password)
	if err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	if err := ts.services.Roles.SetUserRoles(t.Context(), actor, user.ID, []string{roleID}); err != nil {
		t.Fatalf("assigning role to %s: %v", username, err)
	}
	return user.ID
}

func (ts *testServer) login(t *testing.T, username, password string) string {
	t.Helper()
	resp := ts.doJSON(t, "POST", "/api/v1/auth/login", "", fmt.Sprintf(`{"username":%q,"password":%q}`, username, password))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login for %s: status = %d", username, resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding login response: %v", err)
	}
	return body.AccessToken
}

func (ts *testServer) doJSON(t *testing.T, method, path, bearer, body string) *http.Response {
	t.Helper()
	return ts.doJSONWithHeaders(t, method, path, bearer, body, nil)
}

func (ts *testServer) doJSONWithHeaders(t *testing.T, method, path, bearer, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

const testPassword = "a-sufficiently-long-test-password-1"

func TestLoginHTTPEndToEnd(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	resp := ts.doJSON(t, "POST", "/api/v1/auth/login", "", `{"username":"admin","password":"wrong"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	token := ts.login(t, "admin", testPassword)
	resp = ts.doJSON(t, "GET", "/api/v1/auth/session", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("session status = %d, want 200", resp.StatusCode)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	for _, path := range []string{"/api/v1/users", "/api/v1/roles", "/api/v1/tokens", "/api/v1/permissions"} {
		resp := ts.doJSON(t, "GET", path, "", "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("GET %s without credentials = %d, want 401", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// TestAuthorizationMatrix drives every built-in role against a
// representative probe route for each permission family and asserts the
// route's allow/deny behavior always matches the published permission
// catalog in internal/roles, which is the plan's required table-driven
// authorization proof.
func TestAuthorizationMatrix(t *testing.T) {
	type probe struct {
		name       string
		method     string
		path       string
		body       string
		permission string
	}
	probes := []probe{
		{"users.read", "GET", "/api/v1/users", "", roles.PermUsersRead},
		{"roles.read", "GET", "/api/v1/roles", "", roles.PermRolesRead},
		{"permissions.read", "GET", "/api/v1/permissions", "", roles.PermRolesRead},
		{"tokens.read.self", "GET", "/api/v1/tokens", "", roles.PermTokensReadSelf},
		{"tokens.create.self", "POST", "/api/v1/tokens", `{"name":"probe"}`, roles.PermTokensCreateSelf},
	}

	ts := newTestServer(t)
	ts.bootstrapAdmin(t, "admin", testPassword)

	for _, br := range roles.BuiltInRoles {
		hasPermission := make(map[string]bool, len(br.Permissions))
		for _, p := range br.Permissions {
			hasPermission[p] = true
		}

		username := "probe-" + br.Name
		ts.createUserWithRole(t, username, testPassword, br.ID)
		token := ts.login(t, username, testPassword)

		for _, p := range probes {
			t.Run(br.Name+"/"+p.name, func(t *testing.T) {
				resp := ts.doJSON(t, p.method, p.path, token, p.body)
				defer resp.Body.Close()

				denied := resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized
				wantDenied := !hasPermission[p.permission]
				if denied != wantDenied {
					t.Errorf("role %s calling %s %s (needs %s, has=%v): status=%d, denied=%v, want denied=%v",
						br.Name, p.method, p.path, p.permission, hasPermission[p.permission], resp.StatusCode, denied, wantDenied)
				}
			})
		}
	}
}

func TestLastAdministratorInvariantOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	adminID := ts.bootstrapAdmin(t, "admin", testPassword)
	token := ts.login(t, "admin", testPassword)

	resp := ts.doJSON(t, "POST", "/api/v1/users/"+adminID+"/disable", token, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("disabling the last administrator over HTTP = %d, want 409", resp.StatusCode)
	}
}
