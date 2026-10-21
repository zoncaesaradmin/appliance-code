package keys

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

func TestLoadAPITokenPepperReadsOnlyRequiredKey(t *testing.T) {
	dir := t.TempDir()
	material, err := LoadOrGenerate(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, configurationArchiveFile)); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAPITokenPepper(dir)
	if err != nil {
		t.Fatalf("LoadAPITokenPepper: %v", err)
	}
	if !bytes.Equal(got, material.APITokenPepper) {
		t.Fatal("loaded API token pepper differs from generated material")
	}
}
