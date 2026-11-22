package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	secret, body := "secret", []byte(`{"object":"whatsapp_business_account"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	if err := VerifySignature(secret, body, "sha256="+hex.EncodeToString(mac.Sum(nil))); err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(secret, body, "sha256="+hex.EncodeToString([]byte("bad"))); err == nil {
		t.Fatal("expected mismatch")
	}
}
