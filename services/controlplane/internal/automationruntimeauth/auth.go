package automationruntimeauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

const HeaderName = "X-Appliance-Internal-Auth"

func TokenFromPepper(pepper []byte) string {
	sum := sha256.Sum256(pepper)
	return hex.EncodeToString(sum[:])
}

func EqualToken(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
