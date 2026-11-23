package identity

import (
	"context"
	"crypto/hmac"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu         sync.Mutex
	challenges map[string]Challenge
	attempts   map[string]int
	sessions   map[[32]byte]Session
}

func newMemoryStore() *memoryStore {
	return &memoryStore{challenges: map[string]Challenge{}, attempts: map[string]int{}, sessions: map[[32]byte]Session{}}
}

func (s *memoryStore) CreateChallenge(_ context.Context, c Challenge, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.challenges[c.ID] = c
	return nil
}

func (s *memoryStore) RevokeChallenge(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.challenges, id)
	return nil
}

func (s *memoryStore) VerifyAndCreateSession(_ context.Context, id string, digest [32]byte, now time.Time, session Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.challenges[id]
	if !ok || !now.Before(c.ExpiresAt) || s.attempts[id] >= 3 {
		return ErrInvalidChallenge
	}
	if !hmac.Equal(c.Digest[:], digest[:]) {
		s.attempts[id]++
		return ErrInvalidChallenge
	}
	delete(s.challenges, id)
	s.sessions[session.TokenHash] = session
	return nil
}

func (s *memoryStore) LookupSession(_ context.Context, token [32]byte, now time.Time) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[token]
	if !ok || !now.Before(session.ExpiresAt) {
		return "", ErrUnauthenticated
	}
	return "user-1", nil
}

func (s *memoryStore) RevokeSession(_ context.Context, token [32]byte, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
	return nil
}

type testSender struct {
	code string
	err  error
}

func (s *testSender) Send(_ context.Context, _ string, code string) error {
	s.code = code
	return s.err
}

func TestOTPChallengeAndSession(t *testing.T) {
	store := newMemoryStore()
	sender := &testSender{}
	service, err := New(store, sender, []byte(strings.Repeat("k", 32)), 5*time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	ctx := context.Background()
	id, err := service.Begin(ctx, "+919876543210", "client-1")
	if err != nil || len(id) != 32 || len(sender.code) != 6 {
		t.Fatalf("challenge: id %q, error %v", id, err)
	}
	wrong := "000000"
	if sender.code == wrong {
		wrong = "999999"
	}
	if _, _, err := service.Verify(ctx, id, wrong); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("wrong code accepted: %v", err)
	}
	token, expiry, err := service.Verify(ctx, id, sender.code)
	if err != nil || !expiry.Equal(now.Add(time.Hour)) {
		t.Fatalf("verification failed: %v", err)
	}
	if user, err := service.UserID(ctx, token); err != nil || user != "user-1" {
		t.Fatalf("session lookup: %q, %v", user, err)
	}
	if _, _, err := service.Verify(ctx, id, sender.code); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("replayed code accepted: %v", err)
	}
	now = expiry
	if _, err := service.UserID(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired session accepted: %v", err)
	}
}

func TestOTPRejectsBadInputAndRevokesFailedDelivery(t *testing.T) {
	store := newMemoryStore()
	sender := &testSender{err: errors.New("delivery error included sensitive 123456")}
	service, err := New(store, sender, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := service.Begin(ctx, "123", "client"); !errors.Is(err, ErrInvalidPhone) {
		t.Fatalf("invalid phone: %v", err)
	}
	if _, err := service.Begin(ctx, "+919876543210", "client"); !errors.Is(err, ErrDelivery) || strings.Contains(err.Error(), "123456") || len(store.challenges) != 0 {
		t.Fatalf("failed delivery left a challenge: %v", err)
	}
	if _, _, err := service.Verify(ctx, "invalid", "123456"); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("invalid id: %v", err)
	}
	if _, err := New(store, sender, []byte("short"), time.Minute, time.Hour); err == nil {
		t.Fatal("short OTP digest key accepted")
	}
}

func TestOTPExpiryAttemptLimitAndConcurrentUse(t *testing.T) {
	store := newMemoryStore()
	sender := &testSender{}
	service, err := New(store, sender, []byte(strings.Repeat("k", 32)), time.Minute, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	ctx := context.Background()
	id, err := service.Begin(ctx, "+919876543210", "client")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, _, err := service.Verify(ctx, id, sender.code); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("expired challenge accepted: %v", err)
	}
	id, err = service.Begin(ctx, "+919876543210", "client")
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if sender.code == wrong {
		wrong = "999999"
	}
	for i := 0; i < 3; i++ {
		if _, _, err := service.Verify(ctx, id, wrong); !errors.Is(err, ErrInvalidChallenge) {
			t.Fatalf("wrong code attempt %d: %v", i, err)
		}
	}
	if _, _, err := service.Verify(ctx, id, sender.code); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("over-limit challenge accepted: %v", err)
	}
	id, err = service.Begin(ctx, "+919876543210", "client")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, _, err := service.Verify(ctx, id, sender.code)
			results <- err
		}()
	}
	successes := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrInvalidChallenge) {
			t.Fatalf("unexpected concurrent verification error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful verifications, want 1", successes)
	}
}
