package internalauth

import (
	"encoding/base64"
	"testing"
)

func TestTokenFromEncodedPepperMatchesRawPepper(t *testing.T) {
	raw := []byte("0123456789abcdef0123456789abcdef")
	got, err := TokenFromEncodedPepper([]byte(base64.StdEncoding.EncodeToString(raw)))
	if err != nil {
		t.Fatalf("TokenFromEncodedPepper() error = %v", err)
	}
	if want := TokenFromPepper(raw); got != want {
		t.Fatalf("TokenFromEncodedPepper() = %q, want %q", got, want)
	}
}

func TestTokenFromEncodedPepperRejectsInvalidEncoding(t *testing.T) {
	if _, err := TokenFromEncodedPepper([]byte("not base64")); err == nil {
		t.Fatal("TokenFromEncodedPepper() accepted invalid base64")
	}
}
