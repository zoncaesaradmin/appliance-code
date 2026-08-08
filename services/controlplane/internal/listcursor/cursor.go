// Package listcursor builds opaque, integrity-protected list pagination
// cursors per ADR 0010: HMAC-signed, query-bound, and time-limited.
package listcursor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const DefaultTTL = 24 * time.Hour

var (
	ErrInvalid = errors.New("listcursor: invalid cursor")
	ErrExpired = errors.New("listcursor: expired cursor")
)

type payload struct {
	Scope  string `json:"scope"`
	Before int64  `json:"before"`
	Exp    int64  `json:"exp"`
}

// Encode signs a before-sequence cursor for the given scope.
func Encode(key []byte, scope string, beforeSequence int64, now time.Time, ttl time.Duration) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("listcursor: empty key")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	body, err := json.Marshal(payload{
		Scope:  scope,
		Before: beforeSequence,
		Exp:    now.UTC().Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	sig := mac.Sum(nil)
	out := make([]byte, 8+len(body)+len(sig))
	binary.BigEndian.PutUint64(out[:8], uint64(len(body)))
	copy(out[8:], body)
	copy(out[8+len(body):], sig)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// Decode verifies and returns the before-sequence from a cursor.
func Decode(key []byte, scope, cursor string, now time.Time) (int64, error) {
	if cursor == "" {
		return 0, ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) < 8+sha256.Size {
		return 0, ErrInvalid
	}
	bodyLen := int(binary.BigEndian.Uint64(raw[:8]))
	if bodyLen <= 0 || 8+bodyLen+sha256.Size != len(raw) {
		return 0, ErrInvalid
	}
	body := raw[8 : 8+bodyLen]
	sig := raw[8+bodyLen:]
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return 0, ErrInvalid
	}
	var p payload
	if err := json.Unmarshal(body, &p); err != nil {
		return 0, ErrInvalid
	}
	if p.Scope != scope || p.Before <= 0 {
		return 0, ErrInvalid
	}
	if now.UTC().Unix() > p.Exp {
		return 0, ErrExpired
	}
	return p.Before, nil
}
