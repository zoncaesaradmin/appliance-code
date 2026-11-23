//go:build integration

package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// This test expects an already migrated disposable database. It never creates
// or drops the schema; a real PostgreSQL run is required before production.
func TestPostgresIdentityLifecycle(t *testing.T) {
	url := os.Getenv("CAPP_TEST_DATABASE_URL")
	if url == "" || os.Getenv("CAPP_TEST_DATABASE_IS_DISPOSABLE") != "yes" {
		t.Skip("set CAPP_TEST_DATABASE_URL and CAPP_TEST_DATABASE_IS_DISPOSABLE=yes")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := NewPostgresStore(db, []byte(strings.Repeat("r", 32)), PostgresLimits{Window: time.Hour, PerPhone: 10, PerClient: 20, MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := NewDevelopmentInbox("test", time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, inbox, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	number, err := rand.Int(rand.Reader, big.NewInt(1000000000))
	if err != nil {
		t.Fatal(err)
	}
	phone := fmt.Sprintf("+919%09d", number.Int64())
	login := func() (string, string, string) {
		t.Helper()
		id, err := service.Begin(ctx, phone, "integration-client-"+phone)
		if err != nil {
			t.Fatal(err)
		}
		code, err := inbox.Take(ctx, phone)
		if err != nil {
			t.Fatal(err)
		}
		token, _, err := service.Verify(ctx, id, code)
		if err != nil {
			t.Fatal(err)
		}
		user, err := service.UserID(ctx, token)
		if err != nil || user == "" {
			t.Fatalf("lookup session: %v", err)
		}
		return id, code, user
	}
	id, code, user := login()
	if _, _, err := service.Verify(ctx, id, code); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("challenge replay: %v", err)
	}
	_, _, secondUser := login()
	if user != secondUser {
		t.Fatal("same phone created a different user ID")
	}
	lockedID, err := service.Begin(ctx, phone, "integration-client-"+phone)
	if err != nil {
		t.Fatal(err)
	}
	correct, err := inbox.Take(ctx, phone)
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if wrong == correct {
		wrong = "999999"
	}
	for i := 0; i < 2; i++ {
		if _, _, err := service.Verify(ctx, lockedID, wrong); !errors.Is(err, ErrInvalidChallenge) {
			t.Fatalf("wrong code attempt %d: %v", i, err)
		}
	}
	if _, _, err := service.Verify(ctx, lockedID, correct); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("over-limit code accepted: %v", err)
	}
}

func TestPostgresConcurrentChallengeLimits(t *testing.T) {
	url := os.Getenv("CAPP_TEST_DATABASE_URL")
	if url == "" || os.Getenv("CAPP_TEST_DATABASE_IS_DISPOSABLE") != "yes" || os.Getenv("CAPP_TEST_POSTGRES_CONCURRENCY") != "yes" {
		t.Skip("requires a disposable multi-connection PostgreSQL database")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := NewPostgresStore(db, []byte(strings.Repeat("r", 32)), PostgresLimits{Window: time.Hour, PerPhone: 2, PerClient: 100, MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := NewDevelopmentInbox("test", time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, inbox, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	number, err := rand.Int(rand.Reader, big.NewInt(1000000000))
	if err != nil {
		t.Fatal(err)
	}
	phone := fmt.Sprintf("+918%09d", number.Int64())
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		go func() {
			_, err := service.Begin(ctx, phone, "concurrent-client-"+phone)
			results <- err
		}()
	}
	successes := 0
	for i := 0; i < cap(results); i++ {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrRateLimited) {
			t.Fatalf("unexpected challenge error: %v", err)
		}
	}
	if successes != 2 {
		t.Fatalf("got %d successful challenges, want 2", successes)
	}
}

func TestPostgresRequestLimiter(t *testing.T) {
	url := os.Getenv("CAPP_TEST_DATABASE_URL")
	if url == "" || os.Getenv("CAPP_TEST_DATABASE_IS_DISPOSABLE") != "yes" {
		t.Skip("requires a disposable database")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	limits := map[string]int{"verify": 2, "challenge": 1}
	key := []byte(strings.Repeat("z", 32))
	a, err := NewPostgresRequestLimiter(db, key, time.Minute, limits)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewPostgresRequestLimiter(db, key, time.Minute, limits)
	if err != nil {
		t.Fatal(err)
	}
	clientBytes := make([]byte, 16)
	if _, err := rand.Read(clientBytes); err != nil {
		t.Fatal(err)
	}
	client := fmt.Sprintf("client-%x", clientBytes)
	for _, limiter := range []*PostgresRequestLimiter{a, b} {
		if allowed, err := limiter.Allow(ctx, client, "verify"); err != nil || !allowed {
			t.Fatalf("allowed attempt: %v %v", allowed, err)
		}
	}
	if allowed, err := a.Allow(ctx, client, "verify"); err != nil || allowed {
		t.Fatalf("shared limit: %v %v", allowed, err)
	}
	if allowed, err := a.Allow(ctx, client, "challenge"); err != nil || !allowed {
		t.Fatalf("independent action: %v %v", allowed, err)
	}
	if _, err := a.Allow(ctx, client, "other"); err == nil {
		t.Fatal("unknown action accepted")
	}
	if _, err := a.Prune(ctx, time.Hour, 100); err == nil {
		t.Fatal("unsafe short retention accepted")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO identity_request_attempts (bucket_key, created_at) VALUES ($1,$2)`, make([]byte, 32), time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if count, err := a.Prune(ctx, 24*time.Hour, 100); err != nil || count < 1 {
		t.Fatalf("prune: %d %v", count, err)
	}
	if allowed, err := b.Allow(ctx, client, "verify"); err != nil || allowed {
		t.Fatalf("pruning changed active limit: %v %v", allowed, err)
	}
}

func TestPostgresConcurrentRequestLimiter(t *testing.T) {
	url := os.Getenv("CAPP_TEST_DATABASE_URL")
	if url == "" || os.Getenv("CAPP_TEST_DATABASE_IS_DISPOSABLE") != "yes" || os.Getenv("CAPP_TEST_POSTGRES_CONCURRENCY") != "yes" {
		t.Skip("requires disposable multi-connection PostgreSQL")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key := []byte(strings.Repeat("z", 32))
	a, err := NewPostgresRequestLimiter(db, key, time.Minute, map[string]int{"verify": 3})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewPostgresRequestLimiter(db, key, time.Minute, map[string]int{"verify": 3})
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	client := fmt.Sprintf("concurrent-%x", random)
	type result struct {
		allowed bool
		err     error
	}
	results := make(chan result, 12)
	for i := 0; i < cap(results); i++ {
		limiter := a
		if i%2 == 0 {
			limiter = b
		}
		go func() { allowed, err := limiter.Allow(ctx, client, "verify"); results <- result{allowed, err} }()
	}
	allowed := 0
	for i := 0; i < cap(results); i++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.allowed {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("allowed %d concurrent requests, want 3", allowed)
	}
}
