package ui

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"appliance-code/services/ui/internal/controlplane"
	uilogging "appliance-code/services/ui/internal/logging"
	"appliance-code/services/ui/internal/session"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

//go:embed templates/*.html templates/partials/*.html static/*
var assets embed.FS

type Config struct {
	ApplianceProfile string
	CookieSecure     bool
	StaticPrefix     string
}

type controlPlane interface {
	Login(ctx context.Context, username, password string) (controlplane.LoginResult, error)
	Refresh(ctx context.Context, refreshToken string) (controlplane.LoginResult, error)
	Logout(ctx context.Context, accessToken string) error
	Session(ctx context.Context, accessToken string) (controlplane.Session, error)
	Version(ctx context.Context) (controlplane.Version, error)
	Ready(ctx context.Context) (controlplane.Health, error)
	SetupStatus(ctx context.Context) (controlplane.SetupStatus, error)
	CreateFirstAdmin(ctx context.Context, username, password, displayName string) error
	ListWorkProfiles(ctx context.Context, accessToken string) ([]controlplane.WorkProfile, error)
	ListWorkspaces(ctx context.Context, accessToken string) ([]controlplane.Workspace, error)
	CurrentWorkspace(ctx context.Context, accessToken string) (controlplane.Workspace, error)
	CreateWorkspace(ctx context.Context, accessToken string, req controlplane.CreateWorkspaceRequest) (controlplane.Workspace, error)
	SetCurrentWorkspace(ctx context.Context, accessToken, workspaceID string) (controlplane.Workspace, error)
	DeleteWorkspace(ctx context.Context, accessToken, workspaceID string) error
	BuilderGitAccess(ctx context.Context, accessToken string) (controlplane.BuilderGitAccessStatus, error)
	ConfigureBuilderGitAccess(ctx context.Context, accessToken string, req controlplane.ConfigureBuilderGitAccessRequest) (controlplane.BuilderGitAccessStatus, error)
	ListCurrentBuildTargets(ctx context.Context, accessToken string) ([]controlplane.BuildTarget, error)
	SubmitCurrentBuild(ctx context.Context, accessToken string, req controlplane.SubmitBuildRequest) (controlplane.Job, error)
	CurrentWorkspaceBuildStatus(ctx context.Context, accessToken string) (controlplane.Job, error)
}

type Server struct {
	cfg       Config
	cp        controlPlane
	sessions  *session.Store
	templates *template.Template
	staticFS  fs.FS
	logger    uilogging.Logger
}

type viewData struct {
	Title            string
	CurrentPath      string
	ApplianceProfile string
	BuilderEnabled   bool
	Error            string
	Message          string
	Session          controlplane.Session
	SetupNeeded      bool
	Version          controlplane.Version
	Health           controlplane.Health
	StatusError      string
}

type builderPageData struct {
	viewData
	WorkProfiles          []controlplane.WorkProfile
	Workspaces            []controlplane.Workspace
	CurrentWorkspace      *controlplane.Workspace
	BuilderGitAccess      controlplane.BuilderGitAccessStatus
	BuildTargets          []controlplane.BuildTarget
	LatestBuild           *controlplane.Job
	CanSubmitBuild        bool
	OpenWorkspaceSettings bool
	SelectedWorkspaceID   string
	SelectedWorkspaceName string
	SelectedWorkProfile   string
	SelectedExisting      bool
	SelectedProfile       *controlplane.WorkProfile
	// WorkspaceStatusPending is true when the current workspace or any
	// listed workspace is still "pending" (provisioning). The overview
	// and workspace-list partials poll live (see /builder/workspaces/live)
	// only while this is true; once every workspace reaches a terminal
	// status, the next poll's response omits the hx-trigger and htmx
	// stops polling on its own.
	WorkspaceStatusPending bool
}

func workspaceStatusPending(current *controlplane.Workspace, workspaces []controlplane.Workspace) bool {
	const statusPending = "pending"
	if current != nil && current.Status == statusPending {
		return true
	}
	for _, ws := range workspaces {
		if ws.Status == statusPending {
			return true
		}
	}
	return false
}

const sessionCookieName = "appliance_ui_session"
const accessTokenRefreshSkew = 30 * time.Second

func New(cfg Config, cp controlPlane, sessions *session.Store, logger uilogging.Logger) (http.Handler, error) {
	if cfg.ApplianceProfile == "" {
		cfg.ApplianceProfile = "core"
	}
	if cfg.StaticPrefix == "" {
		cfg.StaticPrefix = "/static/"
	}
	if cp == nil {
		return nil, errors.New("control plane client is required")
	}
	if logger == nil {
		return nil, errors.New("ui logger is required")
	}
	if sessions == nil {
		sessions = session.NewStore(time.Now)
	}

	tpls, err := template.ParseFS(assets, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}

	s := &Server{cfg: cfg, cp: cp, sessions: sessions, templates: tpls, staticFS: staticFS, logger: logger}
	mux := http.NewServeMux()
	mux.Handle("GET "+cfg.StaticPrefix, http.StripPrefix(strings.TrimSuffix(cfg.StaticPrefix, "/")+"/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /", s.home)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("GET /setup", s.setupPage)
	mux.HandleFunc("POST /setup", s.setup)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /dashboard", s.dashboard)
	mux.HandleFunc("GET /builder/workspaces", s.builderWorkspaces)
	mux.HandleFunc("POST /builder/workspaces", s.createBuilderWorkspace)
	mux.HandleFunc("POST /builder/git-access", s.configureBuilderGitAccess)
	mux.HandleFunc("POST /builder/builds", s.submitBuilderBuild)
	mux.HandleFunc("POST /builder/workspaces/delete", s.deleteBuilderWorkspace)
	mux.HandleFunc("POST /builder/current-workspace", s.setBuilderCurrentWorkspace)
	mux.HandleFunc("GET /partials/builder/work-profile", s.builderWorkProfilePartial)
	mux.HandleFunc("GET /builder/workspaces/live", s.builderWorkspaceLivePartial)
	mux.HandleFunc("GET /partials/status", s.statusPartial)
	mux.HandleFunc("GET /partials/session", s.sessionPartial)
	return withTraceContext(chainMiddleware(securityHeaders(mux), logger)), nil
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if _, err := s.cp.Ready(r.Context()); err != nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ready\n"))
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if rec, ok := s.currentRecord(r); ok {
		data := s.dashboardData(r, rec)
		s.render(w, r, http.StatusOK, "dashboard.html", data)
		return
	}
	initialized, err := s.isInitialized(r.Context())
	if err != nil {
		s.renderLoginError(w, r, "Appliance setup state is not available yet.")
		return
	}
	if !initialized {
		s.renderSetup(w, r, http.StatusOK, "")
		return
	}
	s.renderLogin(w, r, http.StatusOK, "")
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentRecord(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	initialized, err := s.isInitialized(r.Context())
	if err != nil {
		s.renderLoginError(w, r, "Appliance setup state is not available yet.")
		return
	}
	if !initialized {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	s.renderLogin(w, r, http.StatusOK, "")
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderLoginError(w, r, "Could not read the submitted form.")
		return
	}
	username := strings.TrimSpace(r.Form.Get("username"))
	password := r.Form.Get("password")
	if username == "" || password == "" {
		s.renderLoginError(w, r, "Username and password are required.")
		return
	}
	result, err := s.cp.Login(r.Context(), username, password)
	if err != nil {
		s.logger.WithContext(r.Context()).Warnw("login failed", "error", err, "username", username)
		s.renderLoginError(w, r, "Invalid username or password.")
		return
	}
	rec, err := s.sessions.Create(result.AccessToken, result.RefreshToken, result.AccessExpiresAt)
	if err != nil {
		s.logger.WithContext(r.Context()).Errorw("create UI session failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	rec.Username = username
	s.sessions.Update(rec)
	s.logger.WithContext(r.Context()).Infow("login succeeded", "username", username, "sessionID", rec.ID)
	s.setSessionCookie(w, rec.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) setupPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentRecord(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	initialized, err := s.isInitialized(r.Context())
	if err != nil {
		s.renderLoginError(w, r, "Appliance setup state is not available yet.")
		return
	}
	if initialized {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.renderSetup(w, r, http.StatusOK, "")
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderSetup(w, r, http.StatusBadRequest, "Could not read the submitted form.")
		return
	}
	username := strings.TrimSpace(r.Form.Get("username"))
	password := r.Form.Get("password")
	confirm := r.Form.Get("password_confirm")
	if username == "" || password == "" {
		s.renderSetup(w, r, http.StatusBadRequest, "Username and password are required.")
		return
	}
	if password != confirm {
		s.renderSetup(w, r, http.StatusBadRequest, "Passwords did not match.")
		return
	}
	if err := s.cp.CreateFirstAdmin(r.Context(), username, password, ""); err != nil {
		if errors.Is(err, controlplane.ErrAlreadyInitialized) {
			s.renderLogin(w, r, http.StatusConflict, "Appliance is already initialized. Sign in instead.")
			return
		}
		s.logger.WithContext(r.Context()).Warnw("setup create first admin failed", "error", err, "username", username)
		s.renderSetup(w, r, http.StatusBadRequest, "Could not create the first administrator. Check the username and password policy and try again.")
		return
	}
	s.logger.WithContext(r.Context()).Infow("first administrator created", "username", username)
	result, err := s.cp.Login(r.Context(), username, password)
	if err != nil {
		s.logger.WithContext(r.Context()).Warnw("setup login after first admin creation failed", "error", err, "username", username)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	rec, err := s.sessions.Create(result.AccessToken, result.RefreshToken, result.AccessExpiresAt)
	if err != nil {
		s.logger.WithContext(r.Context()).Errorw("create UI session after setup failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	rec.Username = username
	s.sessions.Update(rec)
	s.logger.WithContext(r.Context()).Infow("setup completed", "username", username, "sessionID", rec.ID)
	s.setSessionCookie(w, rec.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if rec, ok := s.currentRecord(r); ok {
		s.logger.WithContext(r.Context()).Infow("logout requested", "sessionID", rec.ID)
		_ = s.cp.Logout(r.Context(), rec.AccessToken)
		s.sessions.Delete(rec.ID)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	data := s.dashboardData(r, rec)
	s.render(w, r, http.StatusOK, "dashboard.html", data)
}

func (s *Server) statusPartial(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	s.render(w, r, http.StatusOK, "status.html", s.dashboardData(r, rec))
}

func (s *Server) sessionPartial(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	data := s.dashboardData(r, rec)
	s.render(w, r, http.StatusOK, "session.html", data)
}

func (s *Server) dashboardData(r *http.Request, rec session.Record) viewData {
	data := viewData{
		Title:            "Dashboard",
		CurrentPath:      r.URL.Path,
		ApplianceProfile: s.cfg.ApplianceProfile,
		BuilderEnabled:   s.builderEnabled(),
	}
	sessionInfo, refreshed, err := s.sessionWithRefresh(r, rec)
	if err != nil {
		data.StatusError = "Session check failed. Please sign in again."
		return data
	}
	if refreshed.ID != "" && (refreshed.AccessToken != rec.AccessToken || refreshed.Username != rec.Username) {
		s.sessions.Update(refreshed)
	}
	data.Session = sessionInfo

	if versionInfo, err := s.cp.Version(r.Context()); err == nil {
		data.Version = versionInfo
	} else {
		data.StatusError = "Version endpoint is not reachable."
	}
	if health, err := s.cp.Ready(r.Context()); err == nil {
		data.Health = health
	} else if data.StatusError == "" {
		data.StatusError = "Control plane is not ready."
	}
	return data
}

func (s *Server) builderWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !s.builderEnabled() {
		http.NotFound(w, r)
		return
	}
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	data := s.builderPageData(r, rec, "", "")
	s.render(w, r, http.StatusOK, "builder_workspaces.html", data)
}

func (s *Server) createBuilderWorkspace(w http.ResponseWriter, r *http.Request) {
	if !s.builderEnabled() {
		http.NotFound(w, r)
		return
	}
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	rec, refreshedOK := s.refreshRecordForAPI(w, r, rec)
	if !refreshedOK {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not read the submitted form."))
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	workProfile := strings.TrimSpace(r.Form.Get("work_profile"))
	if name == "" || workProfile == "" {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Workspace name and workspace profile are required."))
		return
	}
	selectedWorkspaceID := strings.TrimSpace(r.Form.Get("selected_workspace_id"))
	if selectedWorkspaceID != "" && selectedWorkspaceID != "new" {
		s.logger.WithContext(r.Context()).Infow("builder workspace selected by id", "workspaceID", selectedWorkspaceID)
		if _, err := s.cp.SetCurrentWorkspace(r.Context(), rec.AccessToken, selectedWorkspaceID); err != nil {
			s.logger.WithContext(r.Context()).Warnw("set current selected builder workspace failed", "error", err, "workspaceID", selectedWorkspaceID)
			s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not switch the current workspace."))
			return
		}
		http.Redirect(w, r, "/builder/workspaces", http.StatusSeeOther)
		return
	}
	s.logger.WithContext(r.Context()).Infow("builder workspace create-or-select requested", "workspaceName", name, "workProfile", workProfile)
	workspaces, err := s.cp.ListWorkspaces(r.Context(), rec.AccessToken)
	if err != nil {
		s.logger.WithContext(r.Context()).Warnw("list workspaces before set current failed", "error", err, "workspaceName", name, "workProfile", workProfile)
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not load existing workspaces."))
		return
	}
	if sameName, exact := findWorkspaceByNameProfile(workspaces, name, workProfile); exact != nil {
		if _, err := s.cp.SetCurrentWorkspace(r.Context(), rec.AccessToken, exact.ID); err != nil {
			s.logger.WithContext(r.Context()).Warnw("set current existing builder workspace failed", "error", err, "workspaceID", exact.ID, "workspaceName", exact.Name, "workProfile", exact.WorkProfile)
			s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not switch the current workspace."))
			return
		}
		s.logger.WithContext(r.Context()).Infow("builder workspace selected", "workspaceID", exact.ID, "workspaceName", exact.Name, "workProfile", exact.WorkProfile, "selectionMode", "existing")
		http.Redirect(w, r, "/builder/workspaces", http.StatusSeeOther)
		return
	} else if sameName != nil {
		s.render(w, r, http.StatusConflict, "builder_workspaces.html", s.builderPageData(r, rec, "", "A workspace with that name already exists on a different workspace profile. Create a different workspace name instead."))
		return
	}
	if _, err := s.cp.CreateWorkspace(r.Context(), rec.AccessToken, controlplane.CreateWorkspaceRequest{
		Name:        name,
		WorkProfile: workProfile,
	}); err != nil {
		s.logger.WithContext(r.Context()).Warnw("create builder workspace failed", "error", err, "workspaceName", name, "workProfile", workProfile)
		if isHTTPStatus(err, http.StatusPreconditionFailed) {
			s.render(w, r, http.StatusPreconditionFailed, "builder_workspaces.html", s.builderPageData(r, rec, "", "Configure builder Git HTTPS access before creating the first workspace."))
			return
		}
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not create the workspace. Check the workspace profile configuration."))
		return
	}
	s.logger.WithContext(r.Context()).Infow("builder workspace created", "workspaceName", name, "workProfile", workProfile)
	http.Redirect(w, r, "/builder/workspaces", http.StatusSeeOther)
}

func (s *Server) configureBuilderGitAccess(w http.ResponseWriter, r *http.Request) {
	if !s.builderEnabled() {
		http.NotFound(w, r)
		return
	}
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	rec, refreshedOK := s.refreshRecordForAPI(w, r, rec)
	if !refreshedOK {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not read the submitted Git access form."))
		return
	}
	host := strings.TrimSpace(r.Form.Get("git_host"))
	username := strings.TrimSpace(r.Form.Get("git_username"))
	token := strings.TrimSpace(r.Form.Get("git_token"))
	if host == "" || username == "" || token == "" {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Git host, username, and token are required."))
		return
	}
	if _, err := s.cp.ConfigureBuilderGitAccess(r.Context(), rec.AccessToken, controlplane.ConfigureBuilderGitAccessRequest{
		Host:     host,
		Username: username,
		Token:    token,
	}); err != nil {
		s.logger.WithContext(r.Context()).Warnw("configure builder Git access failed", "error", err, "host", host, "username", username)
		if isHTTPStatus(err, http.StatusForbidden) {
			s.render(w, r, http.StatusForbidden, "builder_workspaces.html", s.builderPageData(r, rec, "", "Only an administrator can configure appliance-wide builder Git access."))
			return
		}
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not save builder Git access. Check the host and token and try again."))
		return
	}
	s.logger.WithContext(r.Context()).Infow("builder Git access configured", "host", host, "username", username)
	http.Redirect(w, r, "/builder/workspaces", http.StatusSeeOther)
}

func (s *Server) submitBuilderBuild(w http.ResponseWriter, r *http.Request) {
	if !s.builderEnabled() {
		http.NotFound(w, r)
		return
	}
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	rec, refreshedOK := s.refreshRecordForAPI(w, r, rec)
	if !refreshedOK {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not read the submitted build form."))
		return
	}
	targetName := strings.TrimSpace(r.Form.Get("target_name"))
	imageTag := strings.TrimSpace(r.Form.Get("image_tag"))
	if targetName == "" {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Select a build target before submitting a build."))
		return
	}
	job, err := s.cp.SubmitCurrentBuild(r.Context(), rec.AccessToken, controlplane.SubmitBuildRequest{
		TargetName: targetName,
		ImageTag:   imageTag,
	})
	if err != nil {
		s.logger.WithContext(r.Context()).Warnw("submit builder build failed", "error", err, "targetName", targetName)
		switch {
		case isHTTPStatus(err, http.StatusPreconditionFailed):
			s.render(w, r, http.StatusPreconditionFailed, "builder_workspaces.html", s.builderPageData(r, rec, "", "Configure builder Git HTTPS access before submitting a build."))
		case isHTTPStatus(err, http.StatusConflict):
			s.render(w, r, http.StatusConflict, "builder_workspaces.html", s.builderPageData(r, rec, "", "The current workspace is not ready for builds yet. Wait for provisioning to finish."))
		case isHTTPStatus(err, http.StatusNotFound):
			s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Unknown build target or no current workspace. Check the catalog mapping and try again."))
		default:
			s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not submit the build. Check the target name and catalog configuration."))
		}
		return
	}
	s.logger.WithContext(r.Context()).Infow("builder build submitted", "targetName", targetName, "jobID", job.ID, "artifactRef", job.ArtifactRef)
	http.Redirect(w, r, "/builder/workspaces", http.StatusSeeOther)
}

func (s *Server) deleteBuilderWorkspace(w http.ResponseWriter, r *http.Request) {
	if !s.builderEnabled() {
		http.NotFound(w, r)
		return
	}
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	rec, refreshedOK := s.refreshRecordForAPI(w, r, rec)
	if !refreshedOK {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not read the submitted form."))
		return
	}
	workspaceID := strings.TrimSpace(r.Form.Get("selected_workspace_id"))
	if workspaceID == "" || workspaceID == "new" {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Select an existing workspace before deleting it."))
		return
	}
	s.logger.WithContext(r.Context()).Infow("builder workspace delete requested", "workspaceID", workspaceID)
	if err := s.cp.DeleteWorkspace(r.Context(), rec.AccessToken, workspaceID); err != nil {
		s.logger.WithContext(r.Context()).Warnw("delete builder workspace failed", "error", err, "workspaceID", workspaceID)
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not delete the selected workspace."))
		return
	}
	s.logger.WithContext(r.Context()).Infow("builder workspace deleted", "workspaceID", workspaceID)
	http.Redirect(w, r, "/builder/workspaces?workspace_id=new", http.StatusSeeOther)
}

func (s *Server) setBuilderCurrentWorkspace(w http.ResponseWriter, r *http.Request) {
	if !s.builderEnabled() {
		http.NotFound(w, r)
		return
	}
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	rec, refreshedOK := s.refreshRecordForAPI(w, r, rec)
	if !refreshedOK {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not read the submitted form."))
		return
	}
	workspaceID := strings.TrimSpace(r.Form.Get("workspace_id"))
	if workspaceID == "" {
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Workspace ID is required."))
		return
	}
	s.logger.WithContext(r.Context()).Infow("builder current workspace set requested", "workspaceID", workspaceID)
	if _, err := s.cp.SetCurrentWorkspace(r.Context(), rec.AccessToken, workspaceID); err != nil {
		s.logger.WithContext(r.Context()).Warnw("set current builder workspace failed", "error", err, "workspaceID", workspaceID)
		s.render(w, r, http.StatusBadRequest, "builder_workspaces.html", s.builderPageData(r, rec, "", "Could not switch the current workspace."))
		return
	}
	s.logger.WithContext(r.Context()).Infow("builder current workspace set", "workspaceID", workspaceID)
	http.Redirect(w, r, "/builder/workspaces", http.StatusSeeOther)
}

func (s *Server) builderWorkProfilePartial(w http.ResponseWriter, r *http.Request) {
	if !s.builderEnabled() {
		http.NotFound(w, r)
		return
	}
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	rec, refreshedOK := s.refreshRecordForAPI(w, r, rec)
	if !refreshedOK {
		return
	}
	profiles, err := s.cp.ListWorkProfiles(r.Context(), rec.AccessToken)
	if err != nil {
		s.render(w, r, http.StatusOK, "builder_work_profile_preview.html", builderPageData{})
		return
	}
	data := builderPageData{
		SelectedWorkProfile: strings.TrimSpace(r.URL.Query().Get("work_profile")),
	}
	if data.SelectedWorkProfile != "" {
		data.SelectedProfile = findWorkProfile(profiles, data.SelectedWorkProfile)
	}
	s.render(w, r, http.StatusOK, "builder_work_profile_preview.html", data)
}

// builderWorkspaceLivePartial serves the two workspace status regions
// (the current-workspace overview, and the workspace list) that
// self-poll via htmx while any workspace is still pending. It always
// recomputes builderPageData fresh, the same as a full page load, so a
// workspace's status transition is reflected on the very next poll tick
// instead of only on the next full navigation.
func (s *Server) builderWorkspaceLivePartial(w http.ResponseWriter, r *http.Request) {
	if !s.builderEnabled() {
		http.NotFound(w, r)
		return
	}
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	rec, refreshedOK := s.refreshRecordForAPI(w, r, rec)
	if !refreshedOK {
		return
	}
	data := s.builderPageData(r, rec, "", "")
	switch r.URL.Query().Get("region") {
	case "list":
		s.render(w, r, http.StatusOK, "builder_workspace_list.html", data)
	default:
		s.render(w, r, http.StatusOK, "builder_workspace_overview.html", data)
	}
}

func (s *Server) builderPageData(r *http.Request, rec session.Record, message, formError string) builderPageData {
	base := s.builderViewData(r, rec)
	base.Title = "Builder workspaces"
	base.CurrentPath = "/builder/workspaces"
	base.Message = message
	base.Error = formError

	data := builderPageData{viewData: base}
	if base.Session.Username == "" {
		return data
	}
	if latest, ok := s.sessions.Get(rec.ID); ok {
		rec = latest
	}
	profiles, err := s.cp.ListWorkProfiles(r.Context(), rec.AccessToken)
	if err != nil {
		data.StatusError = "Workspace profiles are not available."
		return data
	}
	gitAccess, err := s.cp.BuilderGitAccess(r.Context(), rec.AccessToken)
	if err != nil {
		data.StatusError = "Builder Git access status is not available."
		return data
	}
	workspaces, err := s.cp.ListWorkspaces(r.Context(), rec.AccessToken)
	if err != nil {
		data.StatusError = "Workspaces are not available."
		return data
	}
	current, err := s.cp.CurrentWorkspace(r.Context(), rec.AccessToken)
	if err == nil {
		data.CurrentWorkspace = &current
	} else if !isHTTPStatus(err, http.StatusNotFound) {
		data.StatusError = "Current workspace is not available."
	}
	data.BuilderGitAccess = gitAccess
	data.WorkProfiles = profiles
	data.Workspaces = workspaces
	data.WorkspaceStatusPending = workspaceStatusPending(data.CurrentWorkspace, workspaces)
	if data.CurrentWorkspace != nil && data.CurrentWorkspace.Status == "ready" {
		targets, err := s.cp.ListCurrentBuildTargets(r.Context(), rec.AccessToken)
		if err != nil {
			data.StatusError = "Build targets are not available."
		} else {
			data.BuildTargets = targets
		}
		if job, err := s.cp.CurrentWorkspaceBuildStatus(r.Context(), rec.AccessToken); err == nil {
			data.LatestBuild = &job
		} else if !isHTTPStatus(err, http.StatusNotFound) {
			if data.StatusError == "" {
				data.StatusError = "Latest build status is not available."
			}
		}
		data.CanSubmitBuild = gitAccess.Configured && len(data.BuildTargets) > 0
	}
	selectedID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	data.OpenWorkspaceSettings = !gitAccess.Configured || data.CurrentWorkspace == nil || selectedID != "" || formError != "" || message != ""
	selected, selectedIsNew := selectedBuilderWorkspace(selectedID, workspaces, data.CurrentWorkspace)
	if selectedIsNew {
		data.SelectedWorkspaceID = "new"
	} else if selected != nil {
		data.SelectedWorkspaceID = selected.ID
		data.SelectedWorkspaceName = selected.Name
		data.SelectedWorkProfile = selected.WorkProfile
		data.SelectedExisting = true
	}
	if data.SelectedWorkProfile != "" {
		data.SelectedProfile = findWorkProfile(profiles, data.SelectedWorkProfile)
	}
	return data
}

func (s *Server) builderViewData(r *http.Request, rec session.Record) viewData {
	data := viewData{
		Title:            "Builder workspaces",
		CurrentPath:      r.URL.Path,
		ApplianceProfile: s.cfg.ApplianceProfile,
		BuilderEnabled:   s.builderEnabled(),
	}
	if latest, ok := s.sessions.Get(rec.ID); ok {
		rec = latest
	}
	if rec.Username != "" {
		data.Session = controlplane.Session{Username: rec.Username}
		return data
	}
	sessionInfo, refreshed, err := s.sessionWithRefresh(r, rec)
	if err != nil {
		data.StatusError = "Session check failed. Please sign in again."
		return data
	}
	if refreshed.ID != "" && (refreshed.AccessToken != rec.AccessToken || refreshed.Username != rec.Username) {
		s.sessions.Update(refreshed)
	}
	data.Session = sessionInfo
	return data
}

func selectedBuilderWorkspace(selectedID string, workspaces []controlplane.Workspace, current *controlplane.Workspace) (*controlplane.Workspace, bool) {
	if selectedID == "new" {
		return nil, true
	}
	if selectedID != "" {
		for i := range workspaces {
			if workspaces[i].ID == selectedID {
				return &workspaces[i], false
			}
		}
	}
	if current != nil {
		for i := range workspaces {
			if workspaces[i].ID == current.ID {
				return &workspaces[i], false
			}
		}
	}
	return nil, len(workspaces) == 0
}

func findWorkProfile(profiles []controlplane.WorkProfile, name string) *controlplane.WorkProfile {
	for i := range profiles {
		if strings.EqualFold(strings.TrimSpace(profiles[i].Name), strings.TrimSpace(name)) {
			return &profiles[i]
		}
	}
	return nil
}

func findWorkspaceByNameProfile(workspaces []controlplane.Workspace, name, workProfile string) (*controlplane.Workspace, *controlplane.Workspace) {
	normalizedName := normalizeWorkspaceKey(name)
	normalizedProfile := normalizeWorkspaceKey(workProfile)
	var sameName *controlplane.Workspace
	for i := range workspaces {
		if normalizeWorkspaceKey(workspaces[i].Name) != normalizedName {
			continue
		}
		if normalizeWorkspaceKey(workspaces[i].WorkProfile) == normalizedProfile {
			return sameName, &workspaces[i]
		}
		if sameName == nil {
			sameName = &workspaces[i]
		}
	}
	return sameName, nil
}

func normalizeWorkspaceKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (s *Server) builderEnabled() bool {
	return s.cfg.ApplianceProfile == "builder"
}

func isHTTPStatus(err error, status int) bool {
	var statusErr *controlplane.HTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == status
}

func (s *Server) sessionWithRefresh(r *http.Request, rec session.Record) (controlplane.Session, session.Record, error) {
	sessionInfo, err := s.cp.Session(r.Context(), rec.AccessToken)
	if err == nil {
		rec.Username = sessionInfo.Username
		return sessionInfo, rec, nil
	}
	refreshed, refreshErr := s.cp.Refresh(r.Context(), rec.RefreshToken)
	if refreshErr != nil {
		return controlplane.Session{}, rec, err
	}
	rec.AccessToken = refreshed.AccessToken
	rec.RefreshToken = refreshed.RefreshToken
	rec.AccessExpiresAt = refreshed.AccessExpiresAt
	sessionInfo, err = s.cp.Session(r.Context(), rec.AccessToken)
	rec.Username = sessionInfo.Username
	return sessionInfo, rec, err
}

func (s *Server) refreshRecordForAPI(w http.ResponseWriter, r *http.Request, rec session.Record) (session.Record, bool) {
	if latest, ok := s.sessions.Get(rec.ID); ok {
		rec = latest
	}
	if shouldRefreshAccessToken(rec) {
		refreshed, err := s.cp.Refresh(r.Context(), rec.RefreshToken)
		if err != nil {
			s.logger.WithContext(r.Context()).Warnw("refresh UI session for API call failed", "error", err, "path", r.URL.Path)
			s.clearSessionCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return session.Record{}, false
		}
		rec.AccessToken = refreshed.AccessToken
		rec.RefreshToken = refreshed.RefreshToken
		rec.AccessExpiresAt = refreshed.AccessExpiresAt
		s.sessions.Update(rec)
	}
	return rec, true
}

func shouldRefreshAccessToken(rec session.Record) bool {
	if strings.TrimSpace(rec.AccessToken) == "" {
		return true
	}
	if rec.AccessExpiresAt.IsZero() {
		return false
	}
	return !time.Now().UTC().Add(accessTokenRefreshSkew).Before(rec.AccessExpiresAt)
}

func (s *Server) renderLogin(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.render(w, r, status, "login.html", viewData{
		Title:            "Sign in",
		CurrentPath:      r.URL.Path,
		ApplianceProfile: s.cfg.ApplianceProfile,
		BuilderEnabled:   s.builderEnabled(),
		Error:            message,
	})
}

func (s *Server) renderSetup(w http.ResponseWriter, r *http.Request, status int, message string) {
	s.render(w, r, status, "setup.html", viewData{
		Title:            "Create first administrator",
		CurrentPath:      r.URL.Path,
		ApplianceProfile: s.cfg.ApplianceProfile,
		BuilderEnabled:   s.builderEnabled(),
		Error:            message,
		SetupNeeded:      true,
	})
}

func (s *Server) renderLoginError(w http.ResponseWriter, r *http.Request, message string) {
	s.renderLogin(w, r, http.StatusUnauthorized, message)
}

func (s *Server) requireRecord(w http.ResponseWriter, r *http.Request) (session.Record, bool) {
	rec, ok := s.currentRecord(r)
	if ok {
		return rec, true
	}
	s.clearSessionCookie(w)
	initialized, err := s.isInitialized(r.Context())
	if err == nil && !initialized {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return session.Record{}, false
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
	return session.Record{}, false
}

func (s *Server) currentRecord(r *http.Request) (session.Record, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return session.Record{}, false
	}
	return s.sessions.Get(cookie.Value)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) isInitialized(ctx context.Context) (bool, error) {
	status, err := s.cp.SetupStatus(ctx)
	if err != nil {
		return false, err
	}
	return status.Initialized, nil
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		s.logger.WithContext(r.Context()).Errorw("render template failed", "template", name, "error", err)
	}
}

func withTraceContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := strings.TrimSpace(r.Header.Get(ctxutil.TraceIDHeader))
		if traceID == "" {
			var ctx context.Context
			ctx, traceID = ctxutil.EnsureTraceID(r.Context())
			r = r.WithContext(ctx)
		} else {
			r = r.WithContext(ctxutil.WithTraceID(r.Context(), traceID))
		}
		w.Header().Set(ctxutil.TraceIDHeader, traceID)
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func chainMiddleware(next http.Handler, logger uilogging.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if shouldSuppressUILog(r.URL.Path) {
			return
		}
		logger.WithContext(r.Context()).Infow("ui request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start).String(),
		)
	})
}

func shouldSuppressUILog(path string) bool {
	switch path {
	case "/health/live", "/health/ready":
		return true
	default:
		return false
	}
}
