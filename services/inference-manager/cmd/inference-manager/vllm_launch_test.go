package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMaxModelLenClampsToModelWindow(t *testing.T) {
	if got := resolveMaxModelLen(1024); got != 1024 {
		t.Fatalf("got %d", got)
	}
	if got := resolveMaxModelLen(8192); got != preferredMaxModelLen {
		t.Fatalf("got %d", got)
	}
	if got := resolveMaxModelLen(0); got != preferredMaxModelLen {
		t.Fatalf("got %d", got)
	}
}

func TestClampLaunchMaxModelLenUsesLocalConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_position_embeddings":1024}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := clampLaunchMaxModelLen([]string{"--max-model-len", "2048"}, dir)
	if len(got) != 2 || got[0] != "--max-model-len" || got[1] != "1024" {
		t.Fatalf("got %v", got)
	}
}

func TestEngineExitStatusMatchesGeneration(t *testing.T) {
	m := testManager(t)
	m.controlDir = t.TempDir()
	m.generation = "gen-1"
	if err := os.WriteFile(filepath.Join(m.controlDir, "engine-exit.json"), []byte(`{"generation":"gen-1","exitCode":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	status, ok := m.engineExitedForCurrentGeneration()
	if !ok || status.ExitCode != 1 {
		t.Fatalf("status=%+v ok=%v", status, ok)
	}
	m.generation = "gen-2"
	if _, ok := m.engineExitedForCurrentGeneration(); ok {
		t.Fatal("expected mismatch")
	}
}
