package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerServesHostEndpoints(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "etc/hostname"), "appliance-01\n")
	mustWriteFile(t, filepath.Join(root, "etc/os-release"), "PRETTY_NAME=\"Ubuntu 24.04.2 LTS\"\n")
	mustWriteFile(t, filepath.Join(root, "proc/uptime"), "123.45 999.00\n")
	mustWriteFile(t, filepath.Join(root, "proc/loadavg"), "0.10 0.20 0.30 1/100 42\n")
	mustWriteFile(t, filepath.Join(root, "proc/meminfo"), "MemTotal:       1024 kB\nMemAvailable:    512 kB\n")
	mustWriteFile(t, filepath.Join(root, "proc/version"), "Linux version 6.8.0-test\n")

	handler := NewHandler(root)
	for _, path := range []string{"/healthz", "/internal/v1/host/info", "/internal/v1/host/stats"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/host/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
