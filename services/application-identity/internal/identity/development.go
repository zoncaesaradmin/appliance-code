package identity

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrDevelopmentCodeUnavailable = errors.New("development code unavailable")

type developmentCode struct {
	value     string
	expiresAt time.Time
}

// DevelopmentInbox is an explicitly local/test-only OTP delivery adapter. Its
// codes are memory-only and must never be exposed by a production endpoint.
type DevelopmentInbox struct {
	mu    sync.Mutex
	codes map[string]developmentCode
	ttl   time.Duration
	limit int
	now   func() time.Time
}

func NewDevelopmentInbox(environment string, ttl time.Duration, limit int) (*DevelopmentInbox, error) {
	if (environment != "local" && environment != "test") || ttl <= 0 || ttl > 10*time.Minute || limit <= 0 {
		return nil, errors.New("development OTP inbox requires a local or test environment and bounded capacity")
	}
	return &DevelopmentInbox{codes: map[string]developmentCode{}, ttl: ttl, limit: limit, now: time.Now}, nil
}

func (d *DevelopmentInbox) Send(ctx context.Context, phone, code string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !phonePattern.MatchString(phone) || !codePattern.MatchString(code) {
		return errors.New("invalid development OTP delivery")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	for key, entry := range d.codes {
		if !now.Before(entry.expiresAt) {
			delete(d.codes, key)
		}
	}
	if _, exists := d.codes[phone]; !exists && len(d.codes) >= d.limit {
		return errors.New("development OTP inbox is full")
	}
	d.codes[phone] = developmentCode{value: code, expiresAt: now.Add(d.ttl)}
	return nil
}

// Take retrieves and removes a code. Only a separately protected local
// operator interface may call this method; it is not a public product API.
func (d *DevelopmentInbox) Take(ctx context.Context, phone string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	entry, exists := d.codes[phone]
	delete(d.codes, phone)
	if !exists || !d.now().Before(entry.expiresAt) {
		return "", ErrDevelopmentCodeUnavailable
	}
	return entry.value, nil
}
