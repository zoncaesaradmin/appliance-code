package keys

import (
	"bytes"
	"context"
	"testing"
)

func TestLoadOrGenerateCreatesStableConfigurationArchiveKey(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrGenerate(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerate first: %v", err)
	}
	key, keyID, err := first.ConfigurationArchiveKey(context.Background())
	if err != nil {
		t.Fatalf("ConfigurationArchiveKey: %v", err)
	}
	if len(key) != pepperLength || keyID == "" {
		t.Fatalf("archive key = %d bytes, id %q", len(key), keyID)
	}
	key[0] ^= 0xff
	if bytes.Equal(key, first.ConfigurationArchiveKeyMaterial) {
		t.Fatal("ConfigurationArchiveKey exposed mutable key material")
	}
	second, err := LoadOrGenerate(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerate second: %v", err)
	}
	if !bytes.Equal(first.ConfigurationArchiveKeyMaterial, second.ConfigurationArchiveKeyMaterial) || first.ConfigurationArchiveKeyID != second.ConfigurationArchiveKeyID {
		t.Fatal("configuration archive key changed across reload")
	}
}
