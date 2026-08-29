// Package internalauth authenticates the control-plane-only projection that
// reaches the privileged host-agent daemon through its pod proxy.
package internalauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const HeaderName = "X-Appliance-Internal-Auth"

func TokenFromPepper(pepper []byte) string {
	sum := sha256.Sum256(pepper)
	return hex.EncodeToString(sum[:])
}

// TokenFromEncodedPepper matches the on-disk format written by the appliance
// key manager: base64 text encoding raw key material.
func TokenFromEncodedPepper(encoded []byte) (string, error) {
	pepper, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		return "", fmt.Errorf("decode internal auth pepper: %w", err)
	}
	if len(pepper) == 0 {
		return "", fmt.Errorf("decode internal auth pepper: empty pepper")
	}
	return TokenFromPepper(pepper), nil
}

func EqualToken(got, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
