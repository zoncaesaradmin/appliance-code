package authn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const opaqueCredentialBytes = 32 // 256 bits, per ADR 0005

// GenerateOpaqueCredential creates a new high-entropy opaque credential
// value used for both refresh credentials and password-reset credentials.
// raw is returned to the caller once; only its digest should ever be
// persisted.
func GenerateOpaqueCredential() (raw string, rawBytes []byte, err error) {
	rawBytes = make([]byte, opaqueCredentialBytes)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", nil, fmt.Errorf("authn: generating opaque credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(rawBytes), rawBytes, nil
}

// DecodeOpaqueCredential parses a previously issued opaque credential back
// into raw bytes for digesting and comparison.
func DecodeOpaqueCredential(raw string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("authn: malformed opaque credential encoding")
	}
	return b, nil
}

// DigestOpaqueCredential computes the keyed digest stored for a refresh or
// reset credential, using the pepper dedicated to that purpose.
func DigestOpaqueCredential(rawBytes, pepper []byte) []byte {
	mac := hmac.New(sha256.New, pepper)
	mac.Write(rawBytes)
	return mac.Sum(nil)
}

// VerifyOpaqueCredential reports whether rawBytes, when digested with
// pepper, matches storedDigest, in constant time.
func VerifyOpaqueCredential(rawBytes, pepper, storedDigest []byte) bool {
	return hmac.Equal(DigestOpaqueCredential(rawBytes, pepper), storedDigest)
}
