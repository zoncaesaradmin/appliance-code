package inference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestKubernetesSchedulerResolvesOnlyRegisteredInferenceNode(t *testing.T) {
	const ref = "91f8b95a-2cd5-4f1b-8c33-3f073e2a9937"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/namespaces/inference/services":
			if !strings.Contains(r.URL.RawQuery, "zon.io%2Finference-node-uid%3D"+ref) {
				t.Fatalf("service selector=%q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"inference-node-gpu-a","labels":{"zon.io/inference-node-uid":"` + ref + `","zon.io/inference-node-name":"gpu-a"}},"spec":{"ports":[{"port":8080}]}}]}`))
		case "/api/v1/nodes/gpu-a":
			_, _ = w.Write([]byte(`{"metadata":{"name":"gpu-a","uid":"` + ref + `","labels":{"zon.io/inference-node":"true"}}}`))
		default:
			t.Fatalf("unexpected Kubernetes path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	scheduler := &KubernetesScheduler{baseURL: server.URL, token: "test", client: server.Client(), namespace: "inference"}
	base, err := scheduler.Resolve(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := base.String(), "http://inference-node-gpu-a.inference.svc.cluster.local:8080"; got != want {
		t.Fatalf("target=%q want %q", got, want)
	}
}

func TestKubernetesSchedulerRejectsUnregisteredNodeRef(t *testing.T) {
	scheduler := &KubernetesScheduler{}
	if _, err := scheduler.Resolve(context.Background(), "NODE NAME"); err == nil || !strings.Contains(err.Error(), "nodeRef") {
		t.Fatalf("Resolve invalid node ref error=%v", err)
	}
}

func TestKubernetesSchedulerResolvesInitialManagerByControllerNodeName(t *testing.T) {
	const ref = "91f8b95a-2cd5-4f1b-8c33-3f073e2a9938"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/namespaces/inference/services":
			switch {
			case strings.Contains(r.URL.RawQuery, "inference-node-uid"):
				_, _ = w.Write([]byte(`{"items":[]}`))
			case strings.Contains(r.URL.RawQuery, "inference-node-name"):
				_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"inference-gateway","labels":{"zon.io/inference-node-name":"control-1"}},"spec":{"ports":[{"port":8080}]}}]}`))
			default:
				t.Fatalf("service selector=%q", r.URL.RawQuery)
			}
		case "/api/v1/nodes":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"control-1","uid":"` + ref + `","labels":{"zon.io/inference-node":"true"}}}]}`))
		case "/api/v1/nodes/control-1":
			_, _ = w.Write([]byte(`{"metadata":{"name":"control-1","uid":"` + ref + `","labels":{"zon.io/inference-node":"true"}}}`))
		default:
			t.Fatalf("unexpected Kubernetes path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	scheduler := &KubernetesScheduler{baseURL: server.URL, token: "test", client: server.Client(), namespace: "inference"}
	base, err := scheduler.Resolve(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := base.String(), "http://inference-gateway.inference.svc.cluster.local:8080"; got != want {
		t.Fatalf("target=%q want %q", got, want)
	}
}

type staticNodeScheduler struct{ target *url.URL }

func (s staticNodeScheduler) Resolve(_ context.Context, ref string) (*url.URL, error) {
	if ref != "node-uid" {
		return nil, ErrUnavailable
	}
	return s.target, nil
}
func (s staticNodeScheduler) List(context.Context) ([]Node, error) {
	return []Node{{Ref: "node-uid", Name: "gpu-a", Ready: true}}, nil
}

func TestServiceRoutesNodeLifecycleToSchedulerTarget(t *testing.T) {
	manager := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/models/load" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"state":"loading","modelId":"tiny:latest"}`))
	}))
	defer manager.Close()
	target, _ := url.Parse(manager.URL)
	service, err := New(Config{BaseURL: "http://local.invalid", Engine: "ollama"}, manager.Client(), staticNodeScheduler{target: target})
	if err != nil {
		t.Fatal(err)
	}
	progress, err := service.Load(t.Context(), "tiny:latest", "node-uid")
	if err != nil {
		t.Fatal(err)
	}
	if progress.NodeRef != "node-uid" {
		t.Fatalf("progress=%+v", progress)
	}
}
