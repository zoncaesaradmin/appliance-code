package ui

import (
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"appliance-code/services/ui/internal/controlplane"
	"appliance-code/services/ui/internal/session"
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
}

type Server struct {
	cfg       Config
	cp        controlPlane
	sessions  *session.Store
	templates *template.Template
	staticFS  fs.FS
	logger    *slog.Logger
}

type viewData struct {
	Title            string
	CurrentPath      string
	ApplianceProfile string
	Error            string
	Session          controlplane.Session
	Version          controlplane.Version
	Health           controlplane.Health
	StatusError      string
}

const sessionCookieName = "appliance_ui_session"

func New(cfg Config, cp controlPlane, sessions *session.Store, logger *slog.Logger) (http.Handler, error) {
	if cfg.ApplianceProfile == "" {
		cfg.ApplianceProfile = "core"
	}
	if cfg.StaticPrefix == "" {
		cfg.StaticPrefix = "/static/"
	}
	if cp == nil {
		return nil, errors.New("control plane client is required")
	}
	if sessions == nil {
		sessions = session.NewStore(time.Now)
	}
	if logger == nil {
		logger = slog.Default()
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
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /dashboard", s.dashboard)
	mux.HandleFunc("GET /partials/status", s.statusPartial)
	mux.HandleFunc("GET /partials/session", s.sessionPartial)
	return securityHeaders(mux), nil
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
		s.render(w, http.StatusOK, "dashboard.html", data)
		return
	}
	s.render(w, http.StatusOK, "login.html", viewData{
		Title:            "Sign in",
		CurrentPath:      r.URL.Path,
		ApplianceProfile: s.cfg.ApplianceProfile,
	})
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.currentRecord(r); ok {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	s.render(w, http.StatusOK, "login.html", viewData{
		Title:            "Sign in",
		CurrentPath:      r.URL.Path,
		ApplianceProfile: s.cfg.ApplianceProfile,
	})
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
		s.logger.Warn("login failed", "error", err)
		s.renderLoginError(w, r, "Invalid username or password.")
		return
	}
	rec, err := s.sessions.Create(result.AccessToken, result.RefreshToken, result.AccessExpiresAt)
	if err != nil {
		s.logger.Error("create UI session failed", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	s.setSessionCookie(w, rec.ID)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if rec, ok := s.currentRecord(r); ok {
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
	s.render(w, http.StatusOK, "dashboard.html", data)
}

func (s *Server) statusPartial(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	s.render(w, http.StatusOK, "status.html", s.dashboardData(r, rec))
}

func (s *Server) sessionPartial(w http.ResponseWriter, r *http.Request) {
	rec, ok := s.requireRecord(w, r)
	if !ok {
		return
	}
	data := s.dashboardData(r, rec)
	s.render(w, http.StatusOK, "session.html", data)
}

func (s *Server) dashboardData(r *http.Request, rec session.Record) viewData {
	data := viewData{
		Title:            "Dashboard",
		CurrentPath:      r.URL.Path,
		ApplianceProfile: s.cfg.ApplianceProfile,
	}
	sessionInfo, refreshed, err := s.sessionWithRefresh(r, rec)
	if err != nil {
		data.StatusError = "Session check failed. Please sign in again."
		return data
	}
	if refreshed.ID != "" && refreshed.AccessToken != rec.AccessToken {
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

func (s *Server) sessionWithRefresh(r *http.Request, rec session.Record) (controlplane.Session, session.Record, error) {
	sessionInfo, err := s.cp.Session(r.Context(), rec.AccessToken)
	if err == nil {
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
	return sessionInfo, rec, err
}

func (s *Server) renderLoginError(w http.ResponseWriter, r *http.Request, message string) {
	s.render(w, http.StatusUnauthorized, "login.html", viewData{
		Title:            "Sign in",
		CurrentPath:      r.URL.Path,
		ApplianceProfile: s.cfg.ApplianceProfile,
		Error:            message,
	})
}

func (s *Server) requireRecord(w http.ResponseWriter, r *http.Request) (session.Record, bool) {
	rec, ok := s.currentRecord(r)
	if ok {
		return rec, true
	}
	s.clearSessionCookie(w)
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

func (s *Server) render(w http.ResponseWriter, status int, name string, data viewData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		s.logger.Error("render template failed", "template", name, "error", err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
