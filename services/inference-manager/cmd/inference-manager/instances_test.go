package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultInstancePersistsAndBindsLoadedModel(t *testing.T) {
	m := testManager(t)
	m.engine = "vllm"
	if err := m.setDefaultInstance("org/model"); err != nil {
		t.Fatal(err)
	}
	if got := m.instanceSummaries(); len(got) != 1 || got[0].ID != defaultInstanceID || len(got[0].Models) != 1 || got[0].Models[0] != "org/model" || got[0].Replicas != 1 {
		t.Fatalf("instance summaries = %+v", got)
	}

	restarted := testManager(t)
	restarted.modelsDir = m.modelsDir
	if err := restarted.loadInstanceRegistry(); err != nil {
		t.Fatal(err)
	}
	if got := restarted.instanceSummaries(); len(got) != 1 || got[0].Models[0] != "org/model" {
		t.Fatalf("restarted instance summaries = %+v", got)
	}
	restarted.instanceMu.RLock()
	binding := restarted.instances.Bindings["org/model"]
	restarted.instanceMu.RUnlock()
	if binding.InstanceID != defaultInstanceID {
		t.Fatalf("binding = %+v", binding)
	}
}

func TestLegacyReadyLoadMigratesToDefaultInstance(t *testing.T) {
	m := testManager(t)
	m.engine = "ollama"
	m.finishLoadProgress("ready", "tiny:latest", "Model is ready for use")
	if err := m.migrateLegacyDefaultInstance(); err != nil {
		t.Fatal(err)
	}
	if got := m.instanceSummaries(); len(got) != 1 || got[0].Models[0] != "tiny:latest" {
		t.Fatalf("instance summaries = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(m.modelsDir, ".zon", "instances.json")); err != nil {
		t.Fatalf("persisted registry: %v", err)
	}
}

func TestFailedLegacyLoadDoesNotCreateInstance(t *testing.T) {
	m := testManager(t)
	m.finishLoadProgress("failed", "tiny:latest", "load failed")
	if err := m.migrateLegacyDefaultInstance(); err != nil {
		t.Fatal(err)
	}
	if got := m.instanceSummaries(); len(got) != 0 {
		t.Fatalf("instance summaries = %+v", got)
	}
}

func TestClearDefaultInstanceRemovesMatchingBinding(t *testing.T) {
	m := testManager(t)
	if err := m.setDefaultInstance("org/model"); err != nil {
		t.Fatal(err)
	}
	if err := m.clearDefaultInstance("org/model"); err != nil {
		t.Fatal(err)
	}
	if got := m.instanceSummaries(); len(got) != 0 {
		t.Fatalf("instance summaries = %+v", got)
	}
	m.instanceMu.RLock()
	_, bound := m.instances.Bindings["org/model"]
	m.instanceMu.RUnlock()
	if bound {
		t.Fatal("binding remains after default instance removal")
	}
}
