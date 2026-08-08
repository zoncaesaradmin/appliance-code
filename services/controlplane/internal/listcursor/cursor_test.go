package listcursor_test

import (
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/listcursor"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cursor, err := listcursor.Encode(key, "audit.events", 42, now, listcursor.DefaultTTL)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	before, err := listcursor.Decode(key, "audit.events", cursor, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if before != 42 {
		t.Fatalf("before = %d, want 42", before)
	}
}

func TestDecodeRejectsExpiredAndWrongScope(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cursor, err := listcursor.Encode(key, "audit.events", 7, now, time.Minute)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if _, err := listcursor.Decode(key, "audit.events", cursor, now.Add(2*time.Minute)); err != listcursor.ErrExpired {
		t.Fatalf("expired err = %v, want ErrExpired", err)
	}
	if _, err := listcursor.Decode(key, "other", cursor, now); err != listcursor.ErrInvalid {
		t.Fatalf("scope err = %v, want ErrInvalid", err)
	}
}
