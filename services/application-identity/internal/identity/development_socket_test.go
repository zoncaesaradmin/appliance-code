package identity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDevelopmentSocketOTPWorkflow(t *testing.T) {
	inbox, err := NewDevelopmentInbox("test", time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Unix socket paths have a small platform-specific length limit; keep the
	// parent short on macOS as well as Linux.
	parent, err := os.MkdirTemp("/tmp", "zonited-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(parent); err != nil {
			t.Error(err)
		}
	})
	if _, err := NewDevelopmentSocket("production", inbox, parent); err == nil {
		t.Fatal("production socket accepted")
	}
	if _, err := NewDevelopmentSocket("test", inbox, "relative"); err == nil {
		t.Fatal("relative socket parent accepted")
	}
	s, err := NewDevelopmentSocket("test", inbox, parent)
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- s.Serve() }()
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	info, err := os.Stat(filepath.Dir(s.Path()))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("directory permissions: %v %v", info, err)
	}
	info, err = os.Stat(s.Path())
	if err != nil || info.Mode().Perm() != 0600 || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket permissions: %v %v", info, err)
	}
	service, err := New(newMemoryStore(), inbox, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const phone = "+919876543210"
	id, err := service.Begin(ctx, phone, "local-client")
	if err != nil {
		t.Fatal(err)
	}
	code, err := TakeDevelopmentCode(ctx, s.Path(), phone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TakeDevelopmentCode(ctx, s.Path(), phone); !errors.Is(err, ErrDevelopmentCodeUnavailable) {
		t.Fatalf("code taken twice: %v", err)
	}
	token, _, err := service.Verify(ctx, id, code)
	if err != nil {
		t.Fatal(err)
	}
	if user, err := service.UserID(ctx, token); err != nil || user != "user-1" {
		t.Fatalf("session: %q %v", user, err)
	}
}
