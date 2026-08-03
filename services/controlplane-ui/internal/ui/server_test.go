package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"appliance-code/services/controlplane-ui/internal/controlplane"
	uilogging "appliance-code/services/controlplane-ui/internal/logging"
)

type fakeControlPlane struct {
	readyErr   error
	versionErr error
	version    controlplane.Version
}

func (f fakeControlPlane) Ready(_ context.Context) (controlplane.Health, error) {
	if f.readyErr != nil {
		return controlplane.Health{}, f.readyErr
	}
	return controlplane.Health{Status: "ready"}, nil
}

func (f fakeControlPlane) Version(_ context.Context) (controlplane.Version, error) {
	if f.versionErr != nil {
		return controlplane.Version{}, f.versionErr
	}
	if f.version.Version != "" {
		return f.version, nil
	}
	return controlplane.Version{Version: "1.2.3", Commit: "abc", BuildTime: "2026-01-01T00:00:00Z", GoVersion: "go1.26"}, nil
}

func TestSPARoutesServeIndexAndAssets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "index.html"), "<!doctype html><html><body>controlplane-ui</body></html>")
	mustWriteFile(t, filepath.Join(root, "assets", "app.js"), "console.log('ok');")

	logger, err := uilogging.NewWithWriter("debug", os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{StaticDir: root}, fakeControlPlane{}, logger)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("root serves index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("got status %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "controlplane-ui") {
			t.Fatalf("expected index body, got %q", rec.Body.String())
		}
	})

	t.Run("spa path serves index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/manage/dns", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("got status %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "controlplane-ui") {
			t.Fatalf("expected index body, got %q", rec.Body.String())
		}
	})

	t.Run("asset path serves file", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("got status %d, want 200", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "console.log('ok');") {
			t.Fatalf("expected asset body, got %q", rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
			t.Fatalf("asset Cache-Control = %q, want immutable cache header", got)
		}
	})

	t.Run("api path is not handled by spa fallback", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("got status %d, want 404", rec.Code)
		}
	})

	t.Run("non-get methods are rejected for spa routes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/manage/dns", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("got status %d, want 405", rec.Code)
		}
	})

	t.Run("security headers are applied", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
		}
		if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "connect-src 'self'") {
			t.Fatalf("Content-Security-Policy = %q, want self connect-src", got)
		}
	})
}

func TestReadinessRoutes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "index.html"), "ok")
	logger, err := uilogging.NewWithWriter("debug", os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{StaticDir: root}, fakeControlPlane{}, logger)
	if err != nil {
		t.Fatal(err)
	}

	liveReq := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	liveRec := httptest.NewRecorder()
	handler.ServeHTTP(liveRec, liveReq)
	if liveRec.Code != http.StatusOK {
		t.Fatalf("live got %d, want 200", liveRec.Code)
	}

	readyReq := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	readyRec := httptest.NewRecorder()
	handler.ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("ready got %d, want 200", readyRec.Code)
	}
}

func TestReadinessFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "index.html"), "ok")
	logger, err := uilogging.NewWithWriter("debug", os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{StaticDir: root}, fakeControlPlane{readyErr: errors.New("down")}, logger)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready got %d, want 503", rec.Code)
	}
}

func TestVersionRoute(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "index.html"), "ok")
	logger, err := uilogging.NewWithWriter("debug", os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{StaticDir: root}, fakeControlPlane{
		version: controlplane.Version{Version: "1.4.2", Commit: "deadbeef", BuildTime: "2026-08-01T00:00:00Z", GoVersion: "go1.26"},
	}, logger)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/version", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("version got %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	if !strings.Contains(rec.Body.String(), `"version":"1.4.2"`) {
		t.Fatalf("version body = %q, want product version JSON", rec.Body.String())
	}

	failHandler, err := New(Config{StaticDir: root}, fakeControlPlane{versionErr: errors.New("cp down")}, logger)
	if err != nil {
		t.Fatal(err)
	}
	failRec := httptest.NewRecorder()
	failHandler.ServeHTTP(failRec, httptest.NewRequest(http.MethodGet, "/version", nil))
	if failRec.Code != http.StatusBadGateway {
		t.Fatalf("version failure got %d, want 502", failRec.Code)
	}
}

func TestMissingBundleFailsClosed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	logger, err := uilogging.NewWithWriter("debug", os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Config{StaticDir: root}, fakeControlPlane{}, logger)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/home", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got status %d, want 503", rec.Code)
	}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	logger, err := uilogging.NewWithWriter("debug", os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{StaticDir: t.TempDir()}, nil, logger); err == nil {
		t.Fatal("New with nil control plane should fail")
	}
	if _, err := New(Config{StaticDir: t.TempDir()}, fakeControlPlane{}, nil); err == nil {
		t.Fatal("New with nil logger should fail")
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
