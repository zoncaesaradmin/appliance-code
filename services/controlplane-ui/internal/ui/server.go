package ui

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"appliance-code/services/controlplane-ui/internal/controlplane"
	uilogging "appliance-code/services/controlplane-ui/internal/logging"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

type Config struct {
	StaticDir string
}

type controlPlane interface {
	Ready(ctx context.Context) (controlplane.Health, error)
}

type Server struct {
	cfg        Config
	cp         controlPlane
	logger     uilogging.Logger
	indexPath  string
	assetsRoot string
	assets     http.Handler
}

func New(cfg Config, cp controlPlane, logger uilogging.Logger) (http.Handler, error) {
	if cp == nil {
		return nil, errors.New("control plane client is required")
	}
	if logger == nil {
		return nil, errors.New("ui logger is required")
	}
	if strings.TrimSpace(cfg.StaticDir) == "" {
		cfg.StaticDir = "dist"
	}
	staticDir := filepath.Clean(cfg.StaticDir)
	server := &Server{
		cfg:        cfg,
		cp:         cp,
		logger:     logger,
		indexPath:  filepath.Join(staticDir, "index.html"),
		assetsRoot: staticDir,
		assets:     http.FileServer(http.Dir(staticDir)),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", server.live)
	mux.HandleFunc("/health/ready", server.ready)
	mux.HandleFunc("/", server.spa)
	return withTraceContext(chainMiddleware(securityHeaders(mux), logger)), nil
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if _, err := s.cp.Ready(r.Context()); err != nil {
		s.logger.WithContext(r.Context()).Warnw("UI readiness check failed", "error", err.Error())
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ready\n"))
}

func (s *Server) spa(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if path == "." {
		path = ""
	}
	if path != "" && hasFileExtension(path) {
		s.serveStatic(w, r)
		return
	}
	s.serveIndex(w, r)
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	fullPath := filepath.Join(s.assetsRoot, filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	s.assets.ServeHTTP(w, r)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat(s.indexPath); err != nil {
		s.logger.WithContext(r.Context()).Warnw("UI static bundle missing", "indexPath", s.indexPath, "error", err)
		http.Error(w, "ui bundle is not available", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, s.indexPath)
}

func hasFileExtension(path string) bool {
	base := filepath.Base(path)
	return strings.Contains(base, ".")
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; frame-ancestors 'none'")
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
