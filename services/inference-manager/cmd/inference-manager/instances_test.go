package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadyLocalInstancePublishesAndImportsClusterRoutes(t *testing.T) {
	m := testManager(t)
	m.nodeID = "gpu-a"
	m.engine = "vllm"
	t.Setenv("INFERENCE_MANAGER_ENDPOINT", "http://node-gpu-a.inference.svc.cluster.local:8080")
	store := newTestRoutingRegistry()
	m.routingStore = store
	if _, err := store.Upsert(context.Background(), testClusterInstance("node-gpu-b", "gpu-b", "org/model-b")); err != nil {
		t.Fatal(err)
	}
	m.finishLoadProgress("ready", "org/model-a", "Model is ready for use")
	if err := m.setDefaultInstance("org/model-a"); err != nil {
		t.Fatal(err)
	}
	if got := m.routedModelIDs(); len(got) != 2 || got[0] != "org/model-a" || got[1] != "org/model-b" {
		t.Fatalf("routed model ids = %v", got)
	}
}

func TestClusterRefreshDoesNotRepublishStaleLocalCache(t *testing.T) {
	m := testManager(t)
	m.nodeID = "gpu-a"
	m.routingStore = newTestRoutingRegistry()
	m.instances = instanceRegistry{
		Instances: map[string]modelInstance{"node-gpu-a": testClusterInstance("node-gpu-a", "gpu-a", "org/stale")},
		Bindings:  map[string]modelBinding{"org/stale": {ModelAlias: "org/stale", InstanceID: "node-gpu-a"}},
	}
	if err := m.syncLocalRoutingRegistry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := m.routedModelIDs(); len(got) != 0 {
		t.Fatalf("stale local cache was republished: %v", got)
	}
}

func TestDefaultInstancePersistsAndBindsLoadedModel(t *testing.T) {
	m := testManager(t)
	m.engine = "vllm"
	t.Setenv("INFERENCE_MANAGER_ENDPOINT", "http://inference-node-a.inference.svc.cluster.local:8080")
	if err := m.setDefaultInstance("org/model"); err != nil {
		t.Fatal(err)
	}
	if got := m.instanceSummaries(); len(got) != 1 || got[0].ID != defaultInstanceID || len(got[0].Models) != 1 || got[0].Models[0] != "org/model" || got[0].Replicas != 1 {
		t.Fatalf("instance summaries = %+v", got)
	}
	m.instanceMu.RLock()
	nodeRef := m.instances.Instances[defaultInstanceID].NodeRef
	m.instanceMu.RUnlock()
	if nodeRef != "local" {
		t.Fatalf("default instance nodeRef = %q, want local", nodeRef)
	}
	m.instanceMu.RLock()
	endpointRef := m.instances.Instances[defaultInstanceID].EndpointRef
	m.instanceMu.RUnlock()
	if endpointRef != "http://inference-node-a.inference.svc.cluster.local:8080" {
		t.Fatalf("default instance endpointRef = %q", endpointRef)
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

func TestDefaultInstanceReplacementRemovesStaleBinding(t *testing.T) {
	m := testManager(t)
	if err := m.setDefaultInstance("org/first-model"); err != nil {
		t.Fatal(err)
	}
	if err := m.setDefaultInstance("org/second-model"); err != nil {
		t.Fatal(err)
	}
	m.instanceMu.RLock()
	defer m.instanceMu.RUnlock()
	if len(m.instances.Bindings) != 1 {
		t.Fatalf("bindings=%+v", m.instances.Bindings)
	}
	if _, stale := m.instances.Bindings["org/first-model"]; stale {
		t.Fatalf("stale binding remains: %+v", m.instances.Bindings)
	}
	if binding := m.instances.Bindings["org/second-model"]; binding.InstanceID != defaultInstanceID {
		t.Fatalf("replacement binding=%+v", binding)
	}
}

func TestReplacingLocalDefaultPreservesRemoteBinding(t *testing.T) {
	m := testManager(t)
	m.instances.Instances = map[string]modelInstance{"node-b": {ID: "node-b", NodeRef: "node-b", Models: []string{"org/remote"}, Replicas: 1}}
	m.instances.Bindings = map[string]modelBinding{"org/remote": {ModelAlias: "org/remote", InstanceID: "node-b"}}
	if err := m.setDefaultInstance("org/local"); err != nil {
		t.Fatal(err)
	}
	if binding, ok := m.instances.Bindings["org/remote"]; !ok || binding.InstanceID != "node-b" {
		t.Fatalf("remote binding lost: %+v", m.instances.Bindings)
	}
}

func TestNodeBoundManagerUsesDistinctInstanceID(t *testing.T) {
	m := testManager(t)
	m.nodeID = "gpu-worker-2"
	if err := m.setDefaultInstance("org/model"); err != nil {
		t.Fatal(err)
	}
	if got := m.instanceIDForModel("org/model"); got != "node-gpu-worker-2" {
		t.Fatalf("node-bound instance id = %q", got)
	}
	if _, legacy := m.instances.Instances[defaultInstanceID]; legacy {
		t.Fatalf("legacy singleton key retained: %+v", m.instances.Instances)
	}
}

func TestNodeUIDIsDurableInstanceIdentity(t *testing.T) {
	m := testManager(t)
	m.nodeID = "8ec4f2b0-1234-5678-9abc-def012345678"
	if err := m.setDefaultInstance("org/model"); err != nil {
		t.Fatal(err)
	}
	if got := m.instanceIDForModel("org/model"); got != "node-8ec4f2b0-1234-5678-9abc-def012345678" {
		t.Fatalf("node UID instance id = %q", got)
	}
}

func TestInstanceValidationAllowsOneInstancePerNode(t *testing.T) {
	m := testManager(t)
	m.instances.Instances = map[string]modelInstance{
		defaultInstanceID: {ID: defaultInstanceID, NodeRef: "node-a", Models: []string{"org/one"}, Replicas: 1},
		"node-b":          {ID: "node-b", NodeRef: "node-b", Models: []string{"org/two"}, Replicas: 1},
	}
	if err := m.validateAlphaInstancesLocked(); err != nil {
		t.Fatalf("one model per node rejected: %v", err)
	}
	if !m.normalizeAlphaBindingsLocked() || m.instances.Bindings["org/two"].InstanceID != "node-b" {
		t.Fatalf("bindings = %+v", m.instances.Bindings)
	}
}

func TestAlphaInstanceValidationRejectsUnsupportedLayouts(t *testing.T) {
	tests := []struct {
		name      string
		instances map[string]modelInstance
	}{
		{
			name: "same node",
			instances: map[string]modelInstance{
				defaultInstanceID: {ID: defaultInstanceID, NodeRef: "node-a", Models: []string{"org/one"}, Replicas: 1},
				"second":          {ID: "second", NodeRef: "node-a", Models: []string{"org/two"}, Replicas: 1},
			},
		},
		{
			name: "multiple models",
			instances: map[string]modelInstance{
				defaultInstanceID: {ID: defaultInstanceID, Models: []string{"org/one", "org/two"}, Replicas: 1},
			},
		},
		{
			name: "multiple replicas",
			instances: map[string]modelInstance{
				defaultInstanceID: {ID: defaultInstanceID, Models: []string{"org/one"}, Replicas: 2},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := testManager(t)
			m.instances.Instances = test.instances
			if err := m.validateAlphaInstancesLocked(); err == nil {
				t.Fatal("unsupported layout was accepted")
			}
		})
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
