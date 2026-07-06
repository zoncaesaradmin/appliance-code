package authn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// APITokenPrefix is the non-secret prefix every raw API token starts with,
// matching the plan's `apt_...` example format.
const APITokenPrefix = "apt_"

const (
	lookupIDBytes = 9
	secretBytes   = 32
)

// generateLookupSecretPair creates a new "<prefix><lookupID>.<secret>"
// opaque credential shared by API tokens and password-reset credentials:
// raw is shown once; lookupID is a non-secret value stored alongside the
// digest for O(1) lookup; secret must never be persisted, only digested.
// The two base64url parts are joined with "." rather than "_" because "_"
// is itself part of the base64url alphabet and would make the split
// ambiguous whenever a generated lookupID happened to contain one.
func generateLookupSecretPair(prefix string) (raw, lookupID string, secret []byte, err error) {
	lookupRaw := make([]byte, lookupIDBytes)
	if _, err := rand.Read(lookupRaw); err != nil {
		return "", "", nil, fmt.Errorf("authn: generating credential lookup id: %w", err)
	}
	secret = make([]byte, secretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", "", nil, fmt.Errorf("authn: generating credential secret: %w", err)
	}

	lookupID = base64.RawURLEncoding.EncodeToString(lookupRaw)
	secretPart := base64.RawURLEncoding.EncodeToString(secret)
	raw = prefix + lookupID + "." + secretPart
	return raw, lookupID, secret, nil
}

// parseLookupSecretPair splits a presented credential into its lookup ID
// and secret. It never itself proves the credential is valid.
func parseLookupSecretPair(raw, prefix string) (lookupID string, secret []byte, err error) {
	if !strings.HasPrefix(raw, prefix) {
		return "", nil, fmt.Errorf("authn: credential missing %q prefix", prefix)
	}
	rest := strings.TrimPrefix(raw, prefix)
	lookupID, secretPart, ok := strings.Cut(rest, ".")
	if !ok || lookupID == "" || secretPart == "" {
		return "", nil, fmt.Errorf("authn: malformed credential")
	}
	secret, err = base64.RawURLEncoding.DecodeString(secretPart)
	if err != nil {
		return "", nil, fmt.Errorf("authn: malformed credential secret encoding")
	}
	// lookupID itself is opaque to callers; validate its encoding without
	// exposing the decoded bytes since nothing beyond the raw text is used
	// for lookup.
	if _, err := base64.RawURLEncoding.DecodeString(lookupID); err != nil {
		return "", nil, fmt.Errorf("authn: malformed credential lookup id encoding")
	}
	return lookupID, secret, nil
}

// GenerateAPIToken creates a new opaque API token.
func GenerateAPIToken() (raw, lookupID string, secret []byte, err error) {
	return generateLookupSecretPair(APITokenPrefix)
}

// ParseAPIToken splits a presented raw API token into its lookup ID and
// secret.
func ParseAPIToken(raw string) (lookupID string, secret []byte, err error) {
	return parseLookupSecretPair(raw, APITokenPrefix)
}

// DigestAPITokenSecret computes the keyed digest stored for an API token,
// using a pepper dedicated to API tokens and never reused for any other
// purpose, per ADR 0005.
func DigestAPITokenSecret(secret, pepper []byte) []byte {
	mac := hmac.New(sha256.New, pepper)
	mac.Write(secret)
	return mac.Sum(nil)
}

// VerifyAPITokenDigest reports whether secret, when digested with pepper,
// matches storedDigest, in constant time.
func VerifyAPITokenDigest(secret, pepper, storedDigest []byte) bool {
	return hmac.Equal(DigestAPITokenSecret(secret, pepper), storedDigest)
}
