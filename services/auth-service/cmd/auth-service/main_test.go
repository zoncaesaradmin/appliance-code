package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSessionTokenHashEncodesAsPostgresBytea(t *testing.T) {
	token := "test-session-token"
	want := sha256.Sum256([]byte(token))
	encoded, err := pgtype.NewMap().Encode(pgtype.ByteaOID, pgtype.BinaryFormatCode, sessionTokenHash(token), nil)
	if err != nil {
		t.Fatalf("encode session token hash as bytea: %v", err)
	}
	if !bytes.Equal(encoded, want[:]) {
		t.Fatal("encoded session token hash differs from SHA-256 digest")
	}
}

func TestPostgresOTPSessionLifecycle(t *testing.T) {
	dsn := os.Getenv("AUTH_SERVICE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set AUTH_SERVICE_TEST_DATABASE_URL to an isolated test database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, authSchemaSQL); err != nil {
		t.Fatal(err)
	}
	store := newAuthStore(db, []byte("test-rate-key"))
	id, err := newChallengeID()
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("test-otp-key")
	digest := createChallenge(key, id, "123456")
	if err := store.createChallenge(ctx, id, "+919876543210", digest, time.Now().Add(time.Minute), id); err != nil {
		t.Fatal(err)
	}
	token, err := store.verify(ctx, id, digest, time.Now(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("verify OTP and create session: %v", err)
	}
	if err := store.validSession(ctx, token, time.Now()); err != nil {
		t.Fatalf("validate session: %v", err)
	}
	if _, err := store.verify(ctx, id, digest, time.Now(), time.Now().Add(time.Hour)); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("replayed OTP: %v", err)
	}
	if err := store.revokeSession(ctx, token, time.Now()); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if err := store.validSession(ctx, token, time.Now()); !errors.Is(err, errInvalidChallenge) {
		t.Fatalf("revoked session: %v", err)
	}
}

func handler() http.Handler {
	ready := &atomic.Bool{}
	ready.Store(true)
	return authHandler{ready: ready}
}

func TestHandlerHealthAndCapabilities(t *testing.T) {
	tests := []struct {
		name string
		path string
		want int
	}{
		{name: "live", path: "/health/live", want: http.StatusOK},
		{name: "ready", path: "/health/ready", want: http.StatusOK},
		{name: "capabilities", path: "/api/app/v1/auth/capabilities", want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			res := httptest.NewRecorder()
			handler().ServeHTTP(res, req)
			if res.Code != tt.want {
				t.Fatalf("status = %d, want %d", res.Code, tt.want)
			}
		})
	}
}

func TestLoadConfigDefaultsAddress(t *testing.T) {
	get := func(name string) string {
		switch name {
		case "AUTH_SERVICE_DATABASE_URL":
			return "postgres://example"
		case "AUTH_SERVICE_ORIGIN":
			return "https://app.example.com"
		case "AUTH_SERVICE_OTP_KEY", "AUTH_SERVICE_RATE_KEY":
			return "a2tra2tra2tra2tra2tra2tra2tra2tra2tra2tra2t="
		default:
			return ""
		}
	}
	config, err := loadConfig(get)
	if err != nil {
		t.Fatal(err)
	}
	if got := config.address; got != ":8080" {
		t.Fatalf("address = %q, want :8080", got)
	}
}
