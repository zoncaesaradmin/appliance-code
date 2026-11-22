package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

func VerifySignature(appSecret string, body []byte, header string) error {
	if !strings.HasPrefix(header, "sha256=") {
		return errors.New("missing signature")
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil || len(provided) != sha256.Size {
		return errors.New("invalid signature")
	}
	mac := hmac.New(sha256.New, []byte(appSecret))
	_, _ = mac.Write(body)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("signature does not match")
	}
	return nil
}
