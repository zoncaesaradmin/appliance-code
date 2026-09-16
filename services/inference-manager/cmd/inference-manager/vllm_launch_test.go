package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchArgsForCatalogUsesModelWindow(t *testing.T) {
	got := launchArgsForCatalog(32768)
	if len(got) != 2 || got[0] != "--max-model-len" || got[1] != "32768" {
		t.Fatalf("got %v", got)
	}
	if launchArgsForCatalog(0) != nil {
		t.Fatal("unknown window should omit launch args")
	}
}

func TestClampLaunchMaxModelLenUsesLocalConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_position_embeddings":32768}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := clampLaunchMaxModelLen([]string{"--max-model-len", "2048"}, dir)
	if len(got) != 2 || got[0] != "--max-model-len" || got[1] != "32768" {
		t.Fatalf("legacy 2048 must promote to model window, got %v", got)
	}
}

func TestClampLaunchMaxModelLenCapsAboveModelWindow(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_position_embeddings":1024}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := clampLaunchMaxModelLen([]string{"--max-model-len", "8192"}, dir)
	if len(got) != 2 || got[0] != "--max-model-len" || got[1] != "1024" {
		t.Fatalf("got %v", got)
	}
}

func TestClampLaunchMaxModelLenKeepsExplicitLowerOperatorChoice(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_position_embeddings":32768}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := clampLaunchMaxModelLen([]string{"--max-model-len", "4096"}, dir)
	if len(got) != 2 || got[1] != "4096" {
		t.Fatalf("operator lower window should be kept, got %v", got)
	}
}

func TestClampLaunchMaxModelLenAddsMissingFromModelDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_position_embeddings":8192}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := clampLaunchMaxModelLen(nil, dir)
	if len(got) != 2 || got[0] != "--max-model-len" || got[1] != "8192" {
		t.Fatalf("got %v", got)
	}
}

func TestEffectiveMaxModelLen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"n_positions":2048}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := effectiveMaxModelLen(nil, dir); got != 2048 {
		t.Fatalf("got %d", got)
	}
}
