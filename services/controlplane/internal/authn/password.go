// Package authn implements the appliance's two v1 authentication
// mechanisms: local username/password for interactive login, and opaque API
// tokens for automation, plus the session/refresh JWT machinery that sits
// between them. It does not perform authorization; internal/authz owns that.
package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2idParams is calibrated on the minimum supported appliance host and
// recorded alongside each password hash so a later recalibration doesn't
// invalidate existing credentials.
type Argon2idParams struct {
	MemoryKiB   uint32 `json:"memoryKiB"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
	SaltLength  uint32 `json:"saltLength"`
	KeyLength   uint32 `json:"keyLength"`
}

// DefaultArgon2idParams is a conservative baseline; Phase 0 host-sizing
// evidence may justify recalibrating this for the pinned minimum appliance
// host without invalidating credentials hashed under the previous params.
var DefaultArgon2idParams = Argon2idParams{
	MemoryKiB:   64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

const PasswordAlgorithmArgon2id = "argon2id"

// HashPassword derives an Argon2id hash for password using params, returning
// a fresh random salt and the derived key. The password itself is never
// normalized or trimmed, per the plan's password-policy requirement that
// canonically equivalent strings remain distinct.
func HashPassword(password string, params Argon2idParams) (salt, hash []byte, err error) {
	salt = make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, fmt.Errorf("authn: generating password salt: %w", err)
	}
	hash = argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, params.KeyLength)
	return salt, hash, nil
}

// VerifyPassword recomputes the Argon2id hash for password with the given
// salt and params and compares it to hash in constant time.
func VerifyPassword(password string, salt, hash []byte, params Argon2idParams) bool {
	computed := argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, uint32(len(hash)))
	return subtle.ConstantTimeCompare(computed, hash) == 1
}

// EncodeParams serializes params for storage alongside a password hash.
func EncodeParams(params Argon2idParams) (string, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("authn: encoding argon2id params: %w", err)
	}
	return string(b), nil
}

// DecodeParams parses params previously stored by EncodeParams.
func DecodeParams(raw string) (Argon2idParams, error) {
	var params Argon2idParams
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return Argon2idParams{}, fmt.Errorf("authn: decoding argon2id params: %w", err)
	}
	return params, nil
}

// ValidatePasswordPolicy enforces the ADR 0010 password-length policy.
// Composition rules and forced rotation are deliberately not implemented,
// per that same decision. Breached/default-password rejection is a
// documented follow-up: it requires a maintained denylist that isn't
// available in this environment yet.
func ValidatePasswordPolicy(password string) error {
	length := len([]rune(password))
	if length < 14 {
		return fmt.Errorf("password must be at least 14 characters")
	}
	if length > 128 {
		return fmt.Errorf("password must be at most 128 characters")
	}
	if len(password) > 1024 {
		return fmt.Errorf("password must be at most 1024 bytes")
	}
	return nil
}
