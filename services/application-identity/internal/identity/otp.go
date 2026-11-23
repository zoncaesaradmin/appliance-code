// Package identity implements the shared mobile authentication domain.
package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"
)

var (
	ErrInvalidPhone     = errors.New("invalid phone number")
	ErrInvalidChallenge = errors.New("invalid or expired challenge")
	ErrRateLimited      = errors.New("too many authentication attempts")
	ErrUnauthenticated  = errors.New("unauthenticated")
	ErrDelivery         = errors.New("verification delivery unavailable")
	ErrChallengeCleanup = errors.New("failed to revoke verification challenge")
	phonePattern        = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
	codePattern         = regexp.MustCompile(`^[0-9]{6}$`)
	challengePattern    = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// Challenge contains a keyed digest, never the plaintext one-time code.
type Challenge struct {
	ID        string
	Phone     string
	Digest    [32]byte
	ExpiresAt time.Time
}

// Session stores a hash of the browser token, never the token itself.
type Session struct {
	TokenHash [32]byte
	ExpiresAt time.Time
}

// Store must enforce issuance limits and atomically count verification
// failures, expire/consume challenges, and create a user and session on a
// successful verification. It must reject reused and over-limit challenges.
type Store interface {
	CreateChallenge(context.Context, Challenge, string) error
	RevokeChallenge(context.Context, string) error
	VerifyAndCreateSession(context.Context, string, [32]byte, time.Time, Session) error
	LookupSession(context.Context, [32]byte, time.Time) (string, error)
	RevokeSession(context.Context, [32]byte, time.Time) error
}

// Sender delivers a code without logging it or exposing it to the browser.
type Sender interface {
	Send(context.Context, string, string) error
}

type Service struct {
	store        Store
	sender       Sender
	key          []byte
	challengeTTL time.Duration
	sessionTTL   time.Duration
	now          func() time.Time
}

// New requires explicit durations and a secret used only for OTP digests.
func New(store Store, sender Sender, key []byte, challengeTTL, sessionTTL time.Duration) (*Service, error) {
	if store == nil || sender == nil || len(key) < 32 || challengeTTL <= 0 || challengeTTL > 10*time.Minute || sessionTTL <= 0 {
		return nil, errors.New("invalid authentication configuration")
	}
	return &Service{store: store, sender: sender, key: append([]byte(nil), key...), challengeTTL: challengeTTL, sessionTTL: sessionTTL, now: time.Now}, nil
}

// Begin creates a bounded, one-use challenge and delivers its code. rateKey
// is derived by trusted server code for issuance limits, not a browser header.
func (s *Service) Begin(ctx context.Context, phone, rateKey string) (string, error) {
	if !phonePattern.MatchString(phone) {
		return "", ErrInvalidPhone
	}
	if rateKey == "" {
		return "", errors.New("missing authentication rate limit key")
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return "", fmt.Errorf("generate challenge id: %w", err)
	}
	number, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", fmt.Errorf("generate verification code: %w", err)
	}
	id := hex.EncodeToString(idBytes)
	code := fmt.Sprintf("%06d", number.Int64())
	challenge := Challenge{ID: id, Phone: phone, Digest: s.digest(id, code), ExpiresAt: s.now().Add(s.challengeTTL)}
	if err := s.store.CreateChallenge(ctx, challenge, rateKey); err != nil {
		return "", fmt.Errorf("create challenge: %w", err)
	}
	if err := s.sender.Send(ctx, phone, code); err != nil {
		// Delivery errors may contain the phone or code; never propagate them.
		// The challenge expires even if cleanup fails; it is never returned.
		if cleanupErr := s.store.RevokeChallenge(ctx, id); cleanupErr != nil {
			return "", errors.Join(ErrDelivery, ErrChallengeCleanup)
		}
		return "", ErrDelivery
	}
	return id, nil
}

// Verify consumes a valid challenge and creates a session atomically. The
// caller sets the returned opaque token as a secure, HTTP-only cookie.
func (s *Service) Verify(ctx context.Context, id, code string) (string, time.Time, error) {
	if !challengePattern.MatchString(id) || !codePattern.MatchString(code) {
		return "", time.Time{}, ErrInvalidChallenge
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	now := s.now()
	session := Session{TokenHash: sha256.Sum256([]byte(token)), ExpiresAt: now.Add(s.sessionTTL)}
	if err := s.store.VerifyAndCreateSession(ctx, id, s.digest(id, code), now, session); err != nil {
		return "", time.Time{}, fmt.Errorf("verify challenge: %w", err)
	}
	return token, session.ExpiresAt, nil
}

// UserID resolves an active session; the store checks revocation and expiry.
func (s *Service) UserID(ctx context.Context, token string) (string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return "", ErrUnauthenticated
	}
	userID, err := s.store.LookupSession(ctx, sha256.Sum256([]byte(token)), s.now())
	if err != nil {
		return "", fmt.Errorf("lookup session: %w", err)
	}
	if userID == "" {
		return "", ErrUnauthenticated
	}
	return userID, nil
}

// SignOut revokes only this browser's session. Malformed or absent tokens are
// already signed out; repeating the operation is safe.
func (s *Service) SignOut(ctx context.Context, token string) error {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return nil
	}
	if err := s.store.RevokeSession(ctx, sha256.Sum256([]byte(token)), s.now()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *Service) digest(id, code string) [32]byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(id + ":" + code))
	var digest [32]byte
	copy(digest[:], mac.Sum(nil))
	return digest
}
