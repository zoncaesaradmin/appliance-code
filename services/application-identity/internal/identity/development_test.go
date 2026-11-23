package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDevelopmentInboxIsIsolatedBoundedAndOneUse(t *testing.T) {
	if _, err := NewDevelopmentInbox("production", time.Minute, 1); err == nil {
		t.Fatal("production inbox accepted")
	}
	inbox, err := NewDevelopmentInbox("local", time.Minute, 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	inbox.now = func() time.Time { return now }
	ctx := context.Background()
	if err := inbox.Send(ctx, "+919876543210", "123456"); err != nil {
		t.Fatal(err)
	}
	if err := inbox.Send(ctx, "+919876543211", "654321"); err == nil {
		t.Fatal("inbox capacity exceeded")
	}
	code, err := inbox.Take(ctx, "+919876543210")
	if err != nil || code != "123456" {
		t.Fatalf("take: %q %v", code, err)
	}
	if _, err := inbox.Take(ctx, "+919876543210"); !errors.Is(err, ErrDevelopmentCodeUnavailable) {
		t.Fatalf("code reused: %v", err)
	}
	if err := inbox.Send(ctx, "+919876543211", "654321"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err := inbox.Take(ctx, "+919876543211"); !errors.Is(err, ErrDevelopmentCodeUnavailable) {
		t.Fatalf("expired code returned: %v", err)
	}
}

func TestDevelopmentInboxWithOTPService(t *testing.T) {
	inbox, err := NewDevelopmentInbox("test", time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(newMemoryStore(), inbox, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := service.Begin(ctx, "+919876543210", "local-client")
	if err != nil {
		t.Fatal(err)
	}
	code, err := inbox.Take(ctx, "+919876543210")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := service.Verify(ctx, id, code)
	if err != nil {
		t.Fatal(err)
	}
	if user, err := service.UserID(ctx, token); err != nil || user != "user-1" {
		t.Fatalf("session: %q, %v", user, err)
	}
}
