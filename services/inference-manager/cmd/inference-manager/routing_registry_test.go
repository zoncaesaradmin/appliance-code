package main

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func newTestRoutingRegistry() *configMapRoutingRegistry {
	return &configMapRoutingRegistry{client: fake.NewSimpleClientset(), namespace: "inference", name: "appliance-inference-routing"}
}

func testClusterInstance(id, node, model string) modelInstance {
	return modelInstance{
		ID:          id,
		NodeRef:     node,
		RuntimeRef:  "vllm",
		EndpointRef: "http://" + id + ".inference.svc.cluster.local:8080",
		Models:      []string{model},
		Replicas:    1,
	}
}

func TestConfigMapRoutingRegistryPublishesOneReadyModelPerNode(t *testing.T) {
	store := newTestRoutingRegistry()
	ctx := context.Background()
	registry, err := store.Upsert(ctx, testClusterInstance("node-gpu-a", "gpu-a", "org/model-a"))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Instances) != 1 || registry.Bindings["org/model-a"].InstanceID != "node-gpu-a" {
		t.Fatalf("first registry = %+v", registry)
	}
	registry, err = store.Upsert(ctx, testClusterInstance("node-gpu-b", "gpu-b", "org/model-b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Instances) != 2 || registry.Bindings["org/model-b"].InstanceID != "node-gpu-b" {
		t.Fatalf("second registry = %+v", registry)
	}
	loaded, err := store.Load(ctx)
	if err != nil || len(loaded.Instances) != 2 || len(loaded.Bindings) != 2 {
		t.Fatalf("loaded registry = %+v, err=%v", loaded, err)
	}
}

func TestConfigMapRoutingRegistryRejectsSecondModelOnOneNodeAndDuplicateAlias(t *testing.T) {
	store := newTestRoutingRegistry()
	ctx := context.Background()
	if _, err := store.Upsert(ctx, testClusterInstance("node-gpu-a", "gpu-a", "org/model-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(ctx, testClusterInstance("another-gpu-a", "gpu-a", "org/model-b")); err == nil || !strings.Contains(err.Error(), "already has inference instance") {
		t.Fatalf("same node error = %v", err)
	}
	if _, err := store.Upsert(ctx, testClusterInstance("node-gpu-b", "gpu-b", "org/model-a")); err == nil || !strings.Contains(err.Error(), "served by multiple nodes") {
		t.Fatalf("duplicate model error = %v", err)
	}
}

func TestConfigMapRoutingRegistryRemovesPublishedModel(t *testing.T) {
	store := newTestRoutingRegistry()
	ctx := context.Background()
	if _, err := store.Upsert(ctx, testClusterInstance("node-gpu-a", "gpu-a", "org/model-a")); err != nil {
		t.Fatal(err)
	}
	registry, err := store.Remove(ctx, "node-gpu-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Instances) != 0 || len(registry.Bindings) != 0 {
		t.Fatalf("registry after removal = %+v", registry)
	}
}

func TestRoutingNodeUIDUsesKubernetesIdentityNotNodeName(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "bootstrap", UID: types.UID("c2da1e2f-0b31-4e5f-91d7-5d2a4d31f8a9")}})
	got, err := routingNodeUID(context.Background(), client, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	if got != "c2da1e2f-0b31-4e5f-91d7-5d2a4d31f8a9" {
		t.Fatalf("UID = %q", got)
	}
}
