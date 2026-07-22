package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"appliance-code/services/ui/internal/controlplane"
	uilogging "appliance-code/services/ui/internal/logging"
	"appliance-code/services/ui/internal/session"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

type fakeControlPlane struct {
	loginCalls               int
	sessionCalls             int
	versionCalls             int
	readyCalls               int
	listWorkProfilesCalls    int
	listWorkspacesCalls      int
	currentWorkspaceCalls    int
	setCurrentWorkspaceCalls int
	builderGitAccessCalls    int
	configureGitAccessCalls  int
	listBuildTargetsCalls    int
	submitBuildCalls         int
	buildStatusCalls         int
	initialized              bool
	adminUser                string
	adminPass                string
	profiles                 []controlplane.WorkProfile
	workspaces               []controlplane.Workspace
	currentID                string
	builderGitAccess         controlplane.BuilderGitAccessStatus
	buildTargets             []controlplane.BuildTarget
	latestBuild              *controlplane.Job
	submitBuildErr           error
	listBuildTargetsErr      error
	capabilities             []string
	capabilitiesErr          error
	registryRepositories     []string
	registryTags             map[string][]string
	registryReferrers        []controlplane.RegistryReferrer
	registryGrants           []controlplane.RegistryGrant
}

func (f *fakeControlPlane) Capabilities(context.Context) ([]string, error) {
	if f.capabilitiesErr != nil {
		return nil, f.capabilitiesErr
	}
	return append([]string(nil), f.capabilities...), nil
}

func (f *fakeControlPlane) ListRegistryRepositories(context.Context, string) ([]string, error) {
	return append([]string(nil), f.registryRepositories...), nil
}

func (f *fakeControlPlane) ListRegistryTags(_ context.Context, _, repository string) ([]string, error) {
	return append([]string(nil), f.registryTags[repository]...), nil
}

func (f *fakeControlPlane) ListRegistryReferrers(context.Context, string, string, string) ([]controlplane.RegistryReferrer, error) {
	return append([]controlplane.RegistryReferrer(nil), f.registryReferrers...), nil
}

func (f *fakeControlPlane) ListRegistryGrants(context.Context, string) ([]controlplane.RegistryGrant, error) {
	return append([]controlplane.RegistryGrant(nil), f.registryGrants...), nil
}

func (f *fakeControlPlane) CreateRegistryGrant(_ context.Context, _ string, req controlplane.CreateRegistryGrantRequest) (controlplane.RegistryGrant, error) {
	grant := controlplane.RegistryGrant{ID: "grant-new", SubjectType: req.SubjectType, SubjectID: req.SubjectID, PathPrefix: req.PathPrefix, Actions: req.Actions}
	f.registryGrants = append(f.registryGrants, grant)
	return grant, nil
}

func (f *fakeControlPlane) DeleteRegistryGrant(_ context.Context, _, grantID string) error {
	for i := range f.registryGrants {
		if f.registryGrants[i].ID == grantID {
			f.registryGrants = append(f.registryGrants[:i], f.registryGrants[i+1:]...)
			return nil
		}
	}
	return &controlplane.HTTPStatusError{StatusCode: http.StatusNotFound}
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
	f.sessionCalls++
	return controlplane.Session{UserID: "usr_admin", Username: "admin", AuthMethod: "session", Permissions: []string{"users.read"}}, nil
}

func (f *fakeControlPlane) Version(context.Context) (controlplane.Version, error) {
	f.versionCalls++
	return controlplane.Version{Version: "0.1.0", Commit: "abc123", BuildTime: "2026-07-12T00:00:00Z", GoVersion: "go1.26"}, nil
}

func (f *fakeControlPlane) Ready(context.Context) (controlplane.Health, error) {
	f.readyCalls++
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
	f.listWorkProfilesCalls++
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
	f.listWorkspacesCalls++
	return append([]controlplane.Workspace(nil), f.workspaces...), nil
}

func (f *fakeControlPlane) CurrentWorkspace(context.Context, string) (controlplane.Workspace, error) {
	f.currentWorkspaceCalls++
	for _, workspace := range f.workspaces {
		if workspace.ID == f.currentID {
			return workspace, nil
		}
	}
	return controlplane.Workspace{}, &controlplane.HTTPStatusError{Method: http.MethodGet, Path: "/api/v1/current-workspace", StatusCode: http.StatusNotFound}
}

func (f *fakeControlPlane) CreateWorkspace(_ context.Context, _ string, req controlplane.CreateWorkspaceRequest) (controlplane.Workspace, error) {
	workspace := controlplane.Workspace{
		ID:          "ws_" + req.Name,
		OwnerID:     "usr_admin",
		Name:        req.Name,
		WorkProfile: req.WorkProfile,
		Status:      "ready",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	f.workspaces = append(f.workspaces, workspace)
	f.currentID = workspace.ID
	return workspace, nil
}

func (f *fakeControlPlane) SetCurrentWorkspace(_ context.Context, _ string, workspaceID string) (controlplane.Workspace, error) {
	f.setCurrentWorkspaceCalls++
	for _, workspace := range f.workspaces {
		if workspace.ID == workspaceID {
			f.currentID = workspaceID
			return workspace, nil
		}
	}
	return controlplane.Workspace{}, &controlplane.HTTPStatusError{Method: http.MethodPost, Path: "/api/v1/current-workspace", StatusCode: http.StatusNotFound}
}

func (f *fakeControlPlane) DeleteWorkspace(_ context.Context, _ string, workspaceID string) error {
	for i, workspace := range f.workspaces {
		if workspace.ID != workspaceID {
			continue
		}
		f.workspaces = append(f.workspaces[:i], f.workspaces[i+1:]...)
		if f.currentID == workspaceID {
			f.currentID = ""
		}
		return nil
	}
	return &controlplane.HTTPStatusError{Method: http.MethodDelete, Path: "/api/v1/workspaces/" + workspaceID, StatusCode: http.StatusNotFound}
}

func (f *fakeControlPlane) BuilderGitAccess(context.Context, string) (controlplane.BuilderGitAccessStatus, error) {
	f.builderGitAccessCalls++
	if f.builderGitAccess.Host == "" && len(f.builderGitAccess.RequiredHosts) == 0 {
		return controlplane.BuilderGitAccessStatus{
			Configured:    true,
			Host:          "github.com",
			Username:      "builder-user",
			RequiredHosts: []string{"github.com"},
			CanConfigure:  true,
		}, nil
	}
	return f.builderGitAccess, nil
}

func (f *fakeControlPlane) ConfigureBuilderGitAccess(_ context.Context, _ string, req controlplane.ConfigureBuilderGitAccessRequest) (controlplane.BuilderGitAccessStatus, error) {
	f.configureGitAccessCalls++
	f.builderGitAccess = controlplane.BuilderGitAccessStatus{
		Configured:    true,
		Host:          req.Host,
		Username:      req.Username,
		RequiredHosts: []string{req.Host},
		CanConfigure:  true,
	}
	return f.builderGitAccess, nil
}

func (f *fakeControlPlane) ListCurrentBuildTargets(context.Context, string) ([]controlplane.BuildTarget, error) {
	f.listBuildTargetsCalls++
	if f.listBuildTargetsErr != nil {
		return nil, f.listBuildTargetsErr
	}
	return append([]controlplane.BuildTarget(nil), f.buildTargets...), nil
}

func (f *fakeControlPlane) SubmitCurrentBuild(_ context.Context, _ string, req controlplane.SubmitBuildRequest) (controlplane.Job, error) {
	f.submitBuildCalls++
	if f.submitBuildErr != nil {
		return controlplane.Job{}, f.submitBuildErr
	}
	job := controlplane.Job{
		ID:          "job_build_" + req.TargetName,
		Type:        "build",
		Status:      "running",
		TargetName:  req.TargetName,
		ArtifactRef: "users/example/" + req.TargetName + ":" + req.ImageTag,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if req.ImageTag == "" {
		job.ArtifactRef = "users/example/" + req.TargetName + ":latest"
	}
	f.latestBuild = &job
	return job, nil
}

func (f *fakeControlPlane) CurrentWorkspaceBuildStatus(context.Context, string) (controlplane.Job, error) {
	f.buildStatusCalls++
	if f.latestBuild == nil {
		return controlplane.Job{}, &controlplane.HTTPStatusError{Method: http.MethodGet, Path: "/api/v1/current-workspace/build-status", StatusCode: http.StatusNotFound}
	}
	return *f.latestBuild, nil
}

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

const errFakeAuth = fakeErr("invalid credentials")

func testLogger(t *testing.T) uilogging.Logger {
	t.Helper()
	logger, err := uilogging.NewWithWriter("debug", io.Discard)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}
	return logger
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	handler, err := New(Config{ApplianceProfile: "core", CookieSecure: false, StaticPrefix: "/static/"}, &fakeControlPlane{initialized: true, adminUser: "admin", adminPass: "secret", capabilities: capabilitiesForProfile("core")}, session.NewStore(time.Now), testLogger(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return handler
}

// capabilitiesForProfile mirrors, for test purposes only, what the real
// control plane's appliance.profileCatalog resolves for each profile —
// tests pick a profile string and get the matching capability set the
// fake control plane reports, the same way a real deployment would.
func capabilitiesForProfile(profile string) []string {
	switch profile {
	case "builder":
		return []string{"artifact", "base", "build", "workflows"}
	case "storage":
		return []string{"artifact", "base"}
	default:
		return []string{"base", "workflows"}
	}
}

func newTestServerWithProfile(t *testing.T, applianceProfile string, cp *fakeControlPlane) http.Handler {
	t.Helper()
	if cp.capabilities == nil && cp.capabilitiesErr == nil {
		cp.capabilities = capabilitiesForProfile(applianceProfile)
	}
	handler, err := New(Config{ApplianceProfile: applianceProfile, CookieSecure: false, StaticPrefix: "/static/"}, cp, session.NewStore(time.Now), testLogger(t))
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
	handler, err := New(Config{ApplianceProfile: "core", CookieSecure: false, StaticPrefix: "/static/"}, &fakeControlPlane{}, session.NewStore(time.Now), testLogger(t))
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
	handler, err := New(Config{ApplianceProfile: "core", CookieSecure: false, StaticPrefix: "/static/"}, &fakeControlPlane{}, session.NewStore(time.Now), testLogger(t))
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

func TestNewRequiresLogger(t *testing.T) {
	if _, err := New(Config{ApplianceProfile: "core", CookieSecure: false, StaticPrefix: "/static/"}, &fakeControlPlane{}, session.NewStore(time.Now), nil); err == nil {
		t.Fatal("New should reject nil logger")
	}
}

func TestUIRequestTraceIsCreatedAndLogged(t *testing.T) {
	var logBuf bytes.Buffer
	logger, err := uilogging.NewWithWriter("info", &logBuf)
	if err != nil {
		t.Fatalf("NewWithWriter: %v", err)
	}
	handler, err := New(
		Config{ApplianceProfile: "core", CookieSecure: false, StaticPrefix: "/static/"},
		&fakeControlPlane{initialized: true, adminUser: "admin", adminPass: "secret"},
		session.NewStore(time.Now),
		logger,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	traceID := rec.Header().Get(ctxutil.TraceIDHeader)
	if traceID == "" {
		t.Fatal("expected response trace header")
	}

	record := parseUILogRecord(t, logBuf.String(), "ui request")
	if got := record["traceId"]; got != traceID {
		t.Fatalf("traceId = %#v, want %q", got, traceID)
	}
	if got := record["path"]; got != "/login" {
		t.Fatalf("path = %#v, want /login", got)
	}
}

func parseUILogRecord(t *testing.T, text, message string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("parse log JSON: %v\nlog=%s", err, line)
		}
		if record["message"] == message || record["msg"] == message {
			return record
		}
	}
	t.Fatalf("did not find log message %q in %s", message, text)
	return nil
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
	if body := pageRec.Body.String(); !strings.Contains(body, "Workspace profile") || !strings.Contains(body, "Create new workspace") {
		t.Fatalf("builder page body missing workspace controls:\n%s", body)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/builder/workspaces", strings.NewReader("name=demo&work_profile=platform-dev&selected_workspace_id=new"))
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
	if cp.sessionCalls != 0 {
		t.Fatalf("sessionCalls = %d, want 0 for builder create flow", cp.sessionCalls)
	}
}

func TestBuilderWorkspacePagePollsLiveWhilePending(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{
			{ID: "ws_demo", Name: "demo", WorkProfile: "platform-dev", Status: "pending"},
		},
		currentID: "ws_demo",
	}
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
	body := pageRec.Body.String()
	if !strings.Contains(body, `hx-get="/builder/workspaces/live?region=overview`) {
		t.Fatalf("expected overview region to poll live while pending:\n%s", body)
	}
	if !strings.Contains(body, `hx-get="/builder/workspaces/live?region=list`) {
		t.Fatalf("expected workspace list region to poll live while pending:\n%s", body)
	}

	liveOverviewReq := httptest.NewRequest(http.MethodGet, "/builder/workspaces/live?region=overview", nil)
	liveOverviewReq.AddCookie(cookie)
	liveOverviewRec := httptest.NewRecorder()
	handler.ServeHTTP(liveOverviewRec, liveOverviewReq)
	if liveOverviewRec.Code != http.StatusOK || !strings.Contains(liveOverviewRec.Body.String(), "pending") {
		t.Fatalf("live overview partial = %d %q, want 200 containing pending status", liveOverviewRec.Code, liveOverviewRec.Body.String())
	}

	liveListReq := httptest.NewRequest(http.MethodGet, "/builder/workspaces/live?region=list", nil)
	liveListReq.AddCookie(cookie)
	liveListRec := httptest.NewRecorder()
	handler.ServeHTTP(liveListRec, liveListReq)
	if liveListRec.Code != http.StatusOK || !strings.Contains(liveListRec.Body.String(), "demo") {
		t.Fatalf("live list partial = %d %q, want 200 containing workspace demo", liveListRec.Code, liveListRec.Body.String())
	}

	// Once the workflow reaches a terminal status, the next poll response
	// must stop asking htmx to keep polling.
	cp.workspaces[0].Status = "ready"
	readyReq := httptest.NewRequest(http.MethodGet, "/builder/workspaces", nil)
	readyReq.AddCookie(cookie)
	readyRec := httptest.NewRecorder()
	handler.ServeHTTP(readyRec, readyReq)
	readyBody := readyRec.Body.String()
	if strings.Contains(readyBody, "hx-get=\"/builder/workspaces/live") {
		t.Fatalf("expected polling to stop once workspace is ready:\n%s", readyBody)
	}
}

func TestBuilderWorkspacePageKeepsSubmitAvailableWhenGitAccessMissing(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		builderGitAccess: controlplane.BuilderGitAccessStatus{
			Configured:    false,
			RequiredHosts: []string{"github.com"},
			CanConfigure:  true,
		},
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	pageReq := httptest.NewRequest(http.MethodGet, "/builder/workspaces?workspace_id=new", nil)
	pageReq.AddCookie(cookie)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("builder page status = %d, want 200", pageRec.Code)
	}
	body := pageRec.Body.String()
	if !strings.Contains(body, "Workspace creation is blocked until the shared builder Git HTTPS credential is configured above.") {
		t.Fatalf("builder page should explain the Git access prerequisite:\n%s", body)
	}
	if !strings.Contains(body, `<button type="submit">Set Workspace</button>`) {
		t.Fatalf("builder page should keep the Set Workspace submit available for server-side validation:\n%s", body)
	}
	if strings.Contains(body, `disabled>Set Workspace</button>`) || strings.Contains(body, `disabled="">Set Workspace</button>`) {
		t.Fatalf("builder page should not disable the Set Workspace submit:\n%s", body)
	}
}

func TestBuilderWorkspacePagePrefillsSelectedExistingWorkspace(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{
			{ID: "ws_one", Name: "FirstSpace", WorkProfile: "platform-dev", Status: "ready"},
			{ID: "ws_two", Name: "SecondSpace", WorkProfile: "platform-dev", Status: "ready"},
		},
		currentID: "ws_one",
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	pageReq := httptest.NewRequest(http.MethodGet, "/builder/workspaces?workspace_id=ws_two", nil)
	pageReq.AddCookie(cookie)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("builder page status = %d, want 200", pageRec.Code)
	}
	body := pageRec.Body.String()
	if !strings.Contains(body, "Available Workspaces") || !strings.Contains(body, `<details class="workspace-settings " open>`) || !strings.Contains(body, `value="SecondSpace"`) || !strings.Contains(body, `value="ws_two"`) || !strings.Contains(body, "Delete Workspace") || !strings.Contains(body, "Set Workspace") {
		t.Fatalf("builder page did not prefill selected workspace:\n%s", body)
	}
	if cp.versionCalls != 0 || cp.readyCalls != 0 {
		t.Fatalf("builder page should not fetch dashboard status data, got versionCalls=%d readyCalls=%d", cp.versionCalls, cp.readyCalls)
	}
}

func TestBuilderWorkspacePageShowsDeleteOnlyForCurrentWorkspace(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{
			{ID: "ws_one", Name: "FirstSpace", WorkProfile: "platform-dev", Status: "ready"},
			{ID: "ws_two", Name: "SecondSpace", WorkProfile: "platform-dev", Status: "ready"},
		},
		currentID: "ws_one",
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	pageReq := httptest.NewRequest(http.MethodGet, "/builder/workspaces?workspace_id=ws_one", nil)
	pageReq.AddCookie(cookie)
	pageRec := httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if pageRec.Code != http.StatusOK {
		t.Fatalf("builder page status = %d, want 200", pageRec.Code)
	}
	body := pageRec.Body.String()
	if !strings.Contains(body, "Available Workspaces") {
		t.Fatalf("builder page missing workspace list header:\n%s", body)
	}
	if !strings.Contains(body, `name="selected_workspace_id"`) || !strings.Contains(body, "Delete Workspace") || !strings.Contains(body, "Selected workspace") {
		t.Fatalf("builder page should show the selected-workspace delete action for the current workspace:\n%s", body)
	}
	if strings.Contains(body, "Set Workspace") {
		t.Fatalf("builder page should not show Set Workspace for the current workspace:\n%s", body)
	}
}

func TestBuilderWorkspacePageReusesExistingMatchingWorkspace(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{
			{ID: "ws_demo", Name: "DemoSpace", WorkProfile: "platform-dev", Status: "ready"},
		},
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	createReq := httptest.NewRequest(http.MethodPost, "/builder/workspaces", strings.NewReader("name=DemoSpace&work_profile=platform-dev"))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", createRec.Code)
	}
	if cp.currentID != "ws_demo" {
		t.Fatalf("currentID = %q, want ws_demo", cp.currentID)
	}
	if len(cp.workspaces) != 1 {
		t.Fatalf("workspace count = %d, want 1", len(cp.workspaces))
	}
}

func TestBuilderWorkspacePageUsesSelectedWorkspaceIDWithoutListingWorkspaces(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{
			{ID: "ws_one", Name: "FirstSpace", WorkProfile: "platform-dev", Status: "ready"},
			{ID: "ws_two", Name: "SecondSpace", WorkProfile: "platform-dev", Status: "ready"},
		},
		currentID: "ws_one",
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	setReq := httptest.NewRequest(http.MethodPost, "/builder/workspaces", strings.NewReader("selected_workspace_id=ws_two&name=SecondSpace&work_profile=platform-dev"))
	setReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setReq.AddCookie(cookie)
	setRec := httptest.NewRecorder()
	handler.ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", setRec.Code)
	}
	if cp.currentID != "ws_two" {
		t.Fatalf("currentID = %q, want ws_two", cp.currentID)
	}
	if cp.setCurrentWorkspaceCalls != 1 {
		t.Fatalf("setCurrentWorkspaceCalls = %d, want 1", cp.setCurrentWorkspaceCalls)
	}
	if cp.listWorkspacesCalls != 0 {
		t.Fatalf("listWorkspacesCalls = %d, want 0 when selected workspace id is known", cp.listWorkspacesCalls)
	}
}

func TestBuilderWorkspacePageRejectsSameNameDifferentProfile(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{
			{ID: "ws_demo", Name: "DemoSpace", WorkProfile: "platform-dev", Status: "ready"},
		},
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	createReq := httptest.NewRequest(http.MethodPost, "/builder/workspaces", strings.NewReader("name=DemoSpace&work_profile=simulation-dev"))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", createRec.Code)
	}
	if !strings.Contains(createRec.Body.String(), "already exists on a different workspace profile") {
		t.Fatalf("body = %q, want conflict message", createRec.Body.String())
	}
}

func TestBuilderWorkspaceDeleteRemovesSelectedWorkspace(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{
			{ID: "ws_demo", Name: "DemoSpace", WorkProfile: "platform-dev", Status: "ready"},
		},
		currentID: "ws_demo",
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	deleteReq := httptest.NewRequest(http.MethodPost, "/builder/workspaces/delete", strings.NewReader("selected_workspace_id=ws_demo"))
	deleteReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteReq.AddCookie(cookie)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", deleteRec.Code)
	}
	if len(cp.workspaces) != 0 {
		t.Fatalf("workspace count = %d, want 0", len(cp.workspaces))
	}
	if cp.currentID != "" {
		t.Fatalf("currentID = %q, want empty", cp.currentID)
	}
}

func TestBuilderWorkspacePageListsAndSubmitsBuilds(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{{
			ID:          "ws_demo",
			Name:        "demo",
			WorkProfile: "platform-dev",
			Status:      "ready",
		}},
		currentID: "ws_demo",
		buildTargets: []controlplane.BuildTarget{
			{Name: "platformkit", Repo: "platformkit", Execution: "make", Args: []string{"build"}, ImageRepository: "users/example/platformkit"},
			{Name: "platformkit-api", Repo: "platformkit", Execution: "make", Args: []string{"api"}, ImageRepository: "users/example/platformkit-api"},
		},
	}
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
	body := pageRec.Body.String()
	if !strings.Contains(body, "platformkit-api") || !strings.Contains(body, "make build") || !strings.Contains(body, `action="/builder/builds"`) {
		t.Fatalf("builder page missing build targets UI:\n%s", body)
	}
	if cp.listBuildTargetsCalls != 1 {
		t.Fatalf("listBuildTargetsCalls = %d, want 1", cp.listBuildTargetsCalls)
	}
	if cp.buildStatusCalls != 1 {
		t.Fatalf("buildStatusCalls = %d, want 1", cp.buildStatusCalls)
	}

	submitReq := httptest.NewRequest(http.MethodPost, "/builder/builds", strings.NewReader("target_name=platformkit&image_tag=v1"))
	submitReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	submitReq.AddCookie(cookie)
	submitRec := httptest.NewRecorder()
	handler.ServeHTTP(submitRec, submitReq)
	if submitRec.Code != http.StatusSeeOther {
		t.Fatalf("submit status = %d, want 303", submitRec.Code)
	}
	if cp.submitBuildCalls != 1 {
		t.Fatalf("submitBuildCalls = %d, want 1", cp.submitBuildCalls)
	}

	pageReq = httptest.NewRequest(http.MethodGet, "/builder/workspaces", nil)
	pageReq.AddCookie(cookie)
	pageRec = httptest.NewRecorder()
	handler.ServeHTTP(pageRec, pageReq)
	if body := pageRec.Body.String(); !strings.Contains(body, "users/example/platformkit:v1") {
		t.Fatalf("builder page missing latest build artifact:\n%s", body)
	}
}

func TestBuilderWorkspacePageSkipsBuildFetchesUntilReady(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{{
			ID:          "ws_demo",
			Name:        "demo",
			WorkProfile: "platform-dev",
			Status:      "pending",
		}},
		currentID: "ws_demo",
		buildTargets: []controlplane.BuildTarget{
			{Name: "platformkit", Repo: "platformkit", Execution: "make", Args: []string{"build"}},
		},
	}
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
		t.Fatalf("status = %d, want 200", pageRec.Code)
	}
	if cp.listBuildTargetsCalls != 0 || cp.buildStatusCalls != 0 {
		t.Fatalf("unexpected build fetches while pending: targets=%d status=%d", cp.listBuildTargetsCalls, cp.buildStatusCalls)
	}
	if body := pageRec.Body.String(); strings.Contains(body, `action="/builder/builds"`) {
		t.Fatalf("pending workspace should not show submit form:\n%s", body)
	}
}

func TestBuilderSubmitBuildRequiresGitAccess(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{{
			ID:          "ws_demo",
			Name:        "demo",
			WorkProfile: "platform-dev",
			Status:      "ready",
		}},
		currentID: "ws_demo",
		builderGitAccess: controlplane.BuilderGitAccessStatus{
			Configured:    false,
			RequiredHosts: []string{"github.com"},
			CanConfigure:  true,
		},
		buildTargets: []controlplane.BuildTarget{
			{Name: "platformkit", Repo: "platformkit", Execution: "make", Args: []string{"build"}},
		},
		submitBuildErr: &controlplane.HTTPStatusError{Method: http.MethodPost, Path: "/api/v1/current-workspace/builds", StatusCode: http.StatusPreconditionFailed},
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	submitReq := httptest.NewRequest(http.MethodPost, "/builder/builds", strings.NewReader("target_name=platformkit"))
	submitReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	submitReq.AddCookie(cookie)
	submitRec := httptest.NewRecorder()
	handler.ServeHTTP(submitRec, submitReq)
	if submitRec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412", submitRec.Code)
	}
	if body := submitRec.Body.String(); !strings.Contains(body, "Configure builder Git HTTPS access") {
		t.Fatalf("body missing git access error:\n%s", body)
	}
}

func TestBuilderSubmitBuildRejectsUnknownTarget(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true,
		adminUser:   "admin",
		adminPass:   "secret",
		workspaces: []controlplane.Workspace{{
			ID:          "ws_demo",
			Name:        "demo",
			WorkProfile: "platform-dev",
			Status:      "ready",
		}},
		currentID: "ws_demo",
		buildTargets: []controlplane.BuildTarget{
			{Name: "platformkit", Repo: "platformkit", Execution: "make", Args: []string{"build"}},
		},
		submitBuildErr: &controlplane.HTTPStatusError{Method: http.MethodPost, Path: "/api/v1/current-workspace/builds", StatusCode: http.StatusNotFound},
	}
	handler := newTestServerWithProfile(t, "builder", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	submitReq := httptest.NewRequest(http.MethodPost, "/builder/builds", strings.NewReader("target_name=missing"))
	submitReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	submitReq.AddCookie(cookie)
	submitRec := httptest.NewRecorder()
	handler.ServeHTTP(submitRec, submitReq)
	if submitRec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", submitRec.Code)
	}
	if body := submitRec.Body.String(); !strings.Contains(body, "Unknown build target") {
		t.Fatalf("body missing unknown target error:\n%s", body)
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

func TestArtifactPageAvailableWhenCapabilityEnabled(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true, adminUser: "admin", adminPass: "secret",
		registryRepositories: []string{"users/admin/app"},
		registryTags:         map[string][]string{"users/admin/app": {"latest", "v1"}},
		registryReferrers:    []controlplane.RegistryReferrer{{Digest: "sha256:referrer", ArtifactType: "application/spdx+json"}},
		registryGrants:       []controlplane.RegistryGrant{{ID: "grant-1", SubjectType: "user", SubjectID: "usr-1", PathPrefix: "users/admin", Actions: []string{"pull", "push"}}},
	}
	handler := newTestServerWithProfile(t, "storage", cp)

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Artifact Registry") || !strings.Contains(body, "users/admin/app") || !strings.Contains(body, "latest") || !strings.Contains(body, "grant-1") {
		t.Fatalf("artifacts page body missing expected heading:\n%s", body)
	}

	// The Storage profile carries Artifact but not Build (per
	// appliance-code's profileCatalog) — the nav should reflect exactly
	// that capability set, not a "storage means no builder tools at all"
	// assumption baked into a profile-string check.
	navReq := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	navReq.AddCookie(cookie)
	navRec := httptest.NewRecorder()
	handler.ServeHTTP(navRec, navReq)
	if body := navRec.Body.String(); strings.Contains(body, `href="/builder/workspaces"`) {
		t.Fatalf("storage profile has no build capability, should not show the Builder nav link:\n%s", body)
	}
}

func TestArtifactPageNotAvailableWithoutCapability(t *testing.T) {
	handler := newTestServer(t) // core profile: base + workflows only, no artifact
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestArtifactGrantAdministration(t *testing.T) {
	cp := &fakeControlPlane{
		initialized: true, adminUser: "admin", adminPass: "secret",
		registryRepositories: []string{"teams/demo/app"},
		registryTags:         map[string][]string{"teams/demo/app": {"latest"}},
	}
	handler := newTestServerWithProfile(t, "storage", cp)
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("username=admin&password=secret"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	createReq := httptest.NewRequest(http.MethodPost, "/artifacts/grants", strings.NewReader("subject_type=user&subject_id=usr-1&path_prefix=teams/demo&action_pull=on&action_push=on"))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(cookie)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusSeeOther || len(cp.registryGrants) != 1 {
		t.Fatalf("create grant status=%d grants=%+v", createRec.Code, cp.registryGrants)
	}

	deleteReq := httptest.NewRequest(http.MethodPost, "/artifacts/grants/delete", strings.NewReader("grant_id=grant-new"))
	deleteReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteReq.AddCookie(cookie)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusSeeOther || len(cp.registryGrants) != 0 {
		t.Fatalf("delete grant status=%d grants=%+v", deleteRec.Code, cp.registryGrants)
	}
}

func TestBuilderProfileWithoutBuildCapabilityHidesBuilderPage(t *testing.T) {
	// A profile literally named "builder" whose reported capability set
	// doesn't actually include "build" must still be gated off — proving
	// the check is genuinely capability-based rather than a disguised
	// profile-string comparison.
	cp := &fakeControlPlane{
		initialized: true, adminUser: "admin", adminPass: "secret",
		capabilities: []string{"base", "workflows"},
	}
	handler := newTestServerWithProfile(t, "builder", cp)

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
		t.Fatalf("status = %d, want 404 (capability absent despite profile name)", rec.Code)
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
