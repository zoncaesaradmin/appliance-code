package ui

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"appliance-code/services/ui/internal/controlplane"
	"appliance-code/services/ui/internal/session"
)

type fakeControlPlane struct {
	loginCalls  int
	initialized bool
	adminUser   string
	adminPass   string
	profiles    []controlplane.WorkProfile
	workspaces  []controlplane.Workspace
	currentID   string
}

func (f *fakeControlPlane) Login(_ context.Context, username, password string) (controlplane.LoginResult, error) {
	f.loginCalls++
	if !f.initialized || username != f.adminUser || password != f.adminPass {
		return controlplane.LoginResult{}, errFakeAuth
	}
	return controlplane.LoginResult{
		AccessToken:     "access-token",
		RefreshToken:    "refresh-token",
		AccessExpiresAt: time.Now().Add(15 * time.Minute),
	}, nil
}

func (f *fakeControlPlane) Refresh(context.Context, string) (controlplane.LoginResult, error) {
	return controlplane.LoginResult{
		AccessToken:     "new-access-token",
		RefreshToken:    "new-refresh-token",
		AccessExpiresAt: time.Now().Add(15 * time.Minute),
	}, nil
}

func (f *fakeControlPlane) Logout(context.Context, string) error { return nil }

func (f *fakeControlPlane) Session(context.Context, string) (controlplane.Session, error) {
	return controlplane.Session{UserID: "usr_admin", Username: "admin", AuthMethod: "session", Permissions: []string{"users.read"}}, nil
}

func (f *fakeControlPlane) Version(context.Context) (controlplane.Version, error) {
	return controlplane.Version{Version: "0.1.0", Commit: "abc123", BuildTime: "2026-07-12T00:00:00Z", GoVersion: "go1.26"}, nil
}

func (f *fakeControlPlane) Ready(context.Context) (controlplane.Health, error) {
	return controlplane.Health{Status: "ready"}, nil
}

func (f *fakeControlPlane) SetupStatus(context.Context) (controlplane.SetupStatus, error) {
	return controlplane.SetupStatus{Initialized: f.initialized}, nil
}

func (f *fakeControlPlane) CreateFirstAdmin(_ context.Context, username, password, displayName string) error {
	if f.initialized {
		return controlplane.ErrAlreadyInitialized
	}
	f.initialized = true
	f.adminUser = username
	f.adminPass = password
	_ = displayName
	return nil
}

func (f *fakeControlPlane) ListWorkProfiles(context.Context, string) ([]controlplane.WorkProfile, error) {
	if f.profiles == nil {
		return []controlplane.WorkProfile{{
			Name:        "platform-dev",
			Description: "Platform development workspace profile",
			Repos: []controlplane.WorkProfileRepo{
				{Name: "appliance-code", EnabledByDefault: true},
				{Name: "appliance-release"},
			},
		}}, nil
	}
	return f.profiles, nil
}

func (f *fakeControlPlane) ListWorkspaces(context.Context, string) ([]controlplane.Workspace, error) {
	return append([]controlplane.Workspace(nil), f.workspaces...), nil
}

func (f *fakeControlPlane) CurrentWorkspace(context.Context, string) (controlplane.Workspace, error) {
	for _, workspace := range f.workspaces {
		if workspace.ID == f.currentID {
			return workspace, nil
		}
	}
	return controlplane.Workspace{}, &controlplane.HTTPStatusError{Method: http.MethodGet, Path: "/api/v1/current-workspace", StatusCode: http.StatusNotFound}
}

func (f *fakeControlPlane) CreateWorkspace(_ context.Context, _ string, req controlplane.CreateWorkspaceRequest) (controlplane.Workspace, error) {
	workspace := controlplane.Workspace{
		ID:            "ws_" + req.Name,
		OwnerID:       "usr_admin",
		Name:          req.Name,
		WorkProfile:   req.WorkProfile,
		SourceRepoURL: "git@example.invalid:" + req.Repo + ".git",
		SourceRef:     req.SourceRef,
		Status:        "ready",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	f.workspaces = append(f.workspaces, workspace)
	f.currentID = workspace.ID
	return workspace, nil
}

func (f *fakeControlPlane) SetCurrentWorkspace(_ context.Context, _ string, workspaceID string) (controlplane.Workspace, error) {
	for _, workspace := range f.workspaces {
		if workspace.ID == workspaceID {
			f.currentID = workspaceID
			return workspace, nil
		}
	}
	return controlplane.Workspace{}, &controlplane.HTTPStatusError{Method: http.MethodPost, Path: "/api/v1/current-workspace", StatusCode: http.StatusNotFound}
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

const errFakeAuth = fakeErr("invalid credentials")

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	handler, err := New(Config{ApplianceProfile: "core", CookieSecure: false, StaticPrefix: "/static/"}, &fakeControlPlane{initialized: true, adminUser: "admin", adminPass: "secret"}, session.NewStore(time.Now), slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return handler
}

func newTestServerWithProfile(t *testing.T, applianceProfile string, cp *fakeControlPlane) http.Handler {
	t.Helper()
	handler, err := New(Config{ApplianceProfile: applianceProfile, CookieSecure: false, StaticPrefix: "/static/"}, cp, session.NewStore(time.Now), slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return handler
}

func TestLoginPageReturnsHTML(t *testing.T) {
	handler := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Sign in to continue") || strings.Contains(body, "application/json") {
		t.Fatalf("login body does not look like the HTML login page:\n%s", body)
	}
}

func TestRootRouteReturnsBaseHTMLShell(t *testing.T) {
	handler := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<!doctype html>") || !strings.Contains(body, "Sign in to continue") {
		t.Fatalf("root body does not look like the base HTML shell:\n%s", body)
	}
}

func TestRootRouteShowsSetupWhenUninitialized(t *testing.T) {
	handler, err := New(Config{ApplianceProfile: "core", CookieSecure: false, StaticPrefix: "/static/"}, &fakeControlPlane{}, session.NewStore(time.Now), slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Create the first administrator") {
		t.Fatalf("root body does not look like the setup page:\n%s", body)
	}
}

func TestSetupCreatesFirstAdminAndRedirectsToDashboard(t *testing.T) {
	handler, err := New(Config{ApplianceProfile: "core", CookieSecure: false, StaticPrefix: "/static/"}, &fakeControlPlane{}, session.NewStore(time.Now), slog.Default())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader("username=admin&password=secret&password_confirm=secret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/dashboard" {
		t.Fatalf("Location = %q, want /dashboard", got)
	}
}

func TestLoginCreatesOpaqueCookieAndRedirects(t *testing.T) {
	handler := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/dashboard" {
		t.Fatalf("Location = %q, want /dashboard", got)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName {
		t.Fatalf("expected %s cookie, got %+v", sessionCookieName, cookies)
	}
	if strings.Contains(cookies[0].Value, "access-token") || strings.Contains(cookies[0].Value, "refresh-token") {
		t.Fatalf("cookie must be opaque, got %q", cookies[0].Value)
	}
}

func TestDashboardAndPartialsReturnHTML(t *testing.T) {
	handler := newTestServer(t)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/dashboard", want: "Appliance status"},
		{path: "/partials/status", want: "Platform"},
		{path: "/partials/session", want: "Current session"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Fatalf("%s Content-Type = %q, want text/html", tc.path, ct)
		}
		if body := rec.Body.String(); !strings.Contains(body, tc.want) {
			t.Fatalf("%s body missing %q:\n%s", tc.path, tc.want, body)
		}
	}
}

func TestBuilderWorkspacePageCreatesAndSelectsWorkspace(t *testing.T) {
	cp := &fakeControlPlane{initialized: true, adminUser: "admin", adminPass: "secret"}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	pageReq := httptest.NewRequest(http.MethodGet, "/builder/workspaces", nil)
	pageReq.AddCookie(cookie)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("builder page status = %d, want 200", pageRec.Code)
	}
	if body := pageRec.Body.String(); !strings.Contains(body, "Workspace profile and repo") || !strings.Contains(body, "platform-dev / appliance-code") {
		t.Fatalf("builder page body missing workspace controls:\n%s", body)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/builder/workspaces", strings.NewReader("name=demo&profile_repo=platform-dev%7Cappliance-code&source_ref=main"))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, want 303", createRec.Code)
	}
	if got := createRec.Header().Get("Location"); got != "/builder/workspaces" {
		t.Fatalf("Location = %q, want /builder/workspaces", got)
	}
	if cp.currentID != "ws_demo" {
		t.Fatalf("currentID = %q, want ws_demo", cp.currentID)
	}
}

func TestBuilderWorkspacePageNotAvailableForCoreProfile(t *testing.T) {
	handler := newTestServer(t)
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/builder/workspaces", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHealthRoutesReturnPlainText(t *testing.T) {
	handler := newTestServer(t)

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/health/live", want: "ok"},
		{path: "/health/ready", want: "ready"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
			t.Fatalf("%s Content-Type = %q, want text/plain", tc.path, ct)
		}
		if body := rec.Body.String(); !strings.Contains(body, tc.want) {
			t.Fatalf("%s body missing %q:\n%s", tc.path, tc.want, body)
		}
	}
}

func TestDashboardRedirectsToLoginWithoutSession(t *testing.T) {
	handler := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Fatalf("Location = %q, want /login", got)
	}
}

func TestStaticAssetsAreServedLocally(t *testing.T) {
	handler := newTestServer(t)

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/static/app.css", want: "--zon-border"},
		{path: "/static/vendor/htmx.min.js", want: "htmx"},
		{path: "/static/vendor/pico.min.css", want: ":root"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", tc.path, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, tc.want) {
			t.Fatalf("%s body missing %q", tc.path, tc.want)
		}
	}
}
