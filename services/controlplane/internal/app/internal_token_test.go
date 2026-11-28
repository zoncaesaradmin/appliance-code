package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadInternalTokenFile(t *testing.T) {
	if _, err := readInternalTokenFile(""); err == nil {
		t.Fatal("empty path accepted")
	}
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readInternalTokenFile(path); err == nil {
		t.Fatal("short token accepted")
	}
	want := strings.Repeat("a", minimumInternalTokenLength)
	if err := os.WriteFile(path, []byte("  "+want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readInternalTokenFile(path)
	if err != nil || got != want {
		t.Fatalf("token=%q err=%v", got, err)
	}
}
