package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretFileRequiresNonemptyFile(t *testing.T) {
	t.Setenv("GEO_TEST_SECRET_FILE", "")
	if _, err := secretFile("GEO_TEST_SECRET_FILE"); err == nil {
		t.Fatal("missing path accepted")
	}
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEO_TEST_SECRET_FILE", path)
	if _, err := secretFile("GEO_TEST_SECRET_FILE"); err == nil {
		t.Fatal("empty secret accepted")
	}
	if err := os.WriteFile(path, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := secretFile("GEO_TEST_SECRET_FILE")
	if err != nil || got != "token" {
		t.Fatalf("secret=%q err=%v", got, err)
	}
}
