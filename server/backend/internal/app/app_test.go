package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"appliance-code/server/backend/internal/app"
	"appliance-code/server/backend/internal/config"
	"appliance-code/server/backend/internal/logging"
)

// freeAddr asks the OS for an available loopback port and returns it as an
// addr string, closing the listener immediately so the caller can bind it.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("closing probe listener: %v", err)
	}
	return addr
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.PublicAddr = freeAddr(t)
	cfg.InternalAddr = freeAddr(t)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("test config invalid: %v", err)
	}
	return cfg
}

func TestAppStartsServesHealthAndShutsDownCleanly(t *testing.T) {
	cfg := testConfig(t)
	logger, err := logging.New("error")
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	a, err := app.New(cfg, logger)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	waitForListener(t, cfg.InternalAddr)

	assertStatus(t, "http://"+cfg.InternalAddr+"/health/live", http.StatusOK)
	assertStatus(t, "http://"+cfg.InternalAddr+"/health/ready", http.StatusOK)
	assertStatus(t, "http://"+cfg.InternalAddr+"/health/startup", http.StatusOK)
	assertStatus(t, "http://"+cfg.PublicAddr+"/nonexistent", http.StatusNotFound)

	resp, err := http.Get("http://" + cfg.InternalAddr + "/version")
	if err != nil {
		t.Fatalf("GET /version: %v", err)
	}
	defer resp.Body.Close()
	var v struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decoding /version: %v", err)
	}
	if v.Version == "" {
		t.Error("/version returned empty version")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned error after shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of shutdown signal")
	}
}

func waitForListener(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("listener at %s did not come up in time", addr)
}

func assertStatus(t *testing.T, url string, want int) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	if resp.StatusCode != want {
		t.Errorf("GET %s status = %d, want %d", url, resp.StatusCode, want)
	}
}
