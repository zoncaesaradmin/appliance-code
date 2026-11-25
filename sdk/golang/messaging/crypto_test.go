package messaging

import (
	"bytes"
	"testing"
)

func TestSealOpen(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sealed, err := Seal(key, []byte("short-lived otp"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(key, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(opened) != "short-lived otp" {
		t.Fatalf("opened=%q", opened)
	}
	if _, err := Open(bytes.Repeat([]byte{8}, 32), sealed); err == nil {
		t.Fatal("expected authentication failure with a different key")
	}
}
