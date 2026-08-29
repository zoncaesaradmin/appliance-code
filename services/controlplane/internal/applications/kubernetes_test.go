package applications

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestServicesAndNetworkPolicyUseOnlyCatalogEndpoints(t *testing.T) {
	definition := Definition{}
	definition.Metadata.Name = "jellyfin"
	definition.Runtime.Port = 8096
	definition.Runtime.Endpoints = []Endpoint{
		{Name: "http", Protocol: "TCP", Port: 8096, TargetPort: 8096, Direct: true},
		{Name: "discovery", Protocol: "UDP", Port: 7359, TargetPort: 7359, Direct: true},
	}
	labels := map[string]string{"appliance.zon/application": "jellyfin"}
	services := servicesFor(definition, labels, definition.Runtime.Port)
	if len(services) != 2 {
		t.Fatalf("services = %d, want 2", len(services))
	}
	for _, service := range services {
		spec := service["spec"].(map[string]any)
		if spec["type"] != "LoadBalancer" || spec["allocateLoadBalancerNodePorts"] != false {
			t.Fatalf("direct service spec = %#v", spec)
		}
	}
	policy := networkPolicyFor(definition, labels)
	spec := policy["spec"].(map[string]any)
	ingress := spec["ingress"].([]any)[0].(map[string]any)
	ports := ingress["ports"].([]any)
	if len(ports) != 2 {
		t.Fatalf("network policy ports = %#v", ports)
	}
	if ports[1].(map[string]any)["protocol"] != "UDP" || ports[1].(map[string]any)["port"] != 7359 {
		t.Fatalf("discovery port = %#v", ports[1])
	}
}

func TestApplyUsesRuntimeDefaultSeccompProfile(t *testing.T) {
	var deployment map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == "/apis/apps/v1/namespaces/apps/deployments/jellyfin" {
			if err := json.NewDecoder(r.Body).Decode(&deployment); err != nil {
				t.Fatalf("decode deployment: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"status":{"availableReplicas":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	manager := &KubernetesManager{baseURL: server.URL, token: "test-token", client: server.Client()}
	definition := Definition{}
	definition.Metadata.Name = "jellyfin"
	definition.Runtime.Image.Reference = "registry.local/jellyfin@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if observed, err := manager.Apply(context.Background(), definition); err != nil {
		t.Fatalf("Apply() error = %v", err)
	} else if observed != "running" {
		t.Fatalf("Apply() observed state = %q, want running", observed)
	}

	podSpec := deployment["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
	securityContext := podSpec["securityContext"].(map[string]any)
	seccompProfile := securityContext["seccompProfile"].(map[string]any)
	if seccompProfile["type"] != "RuntimeDefault" {
		t.Fatalf("pod securityContext.seccompProfile.type = %q, want RuntimeDefault", seccompProfile["type"])
	}
}

func TestNewInClusterManagerWithoutServiceAccountToken(t *testing.T) {
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		t.Skip("host already mounts an in-cluster SA token; cannot assert missing-token behavior")
	}
	t.Setenv("KUBERNETES_SERVICE_HOST", "kubernetes.default.svc")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")

	manager, err := NewInClusterManager()
	if err != nil {
		t.Fatalf("NewInClusterManager without SA token = %v, want nil error", err)
	}
	if manager != nil {
		t.Fatal("NewInClusterManager without SA token should return a nil manager")
	}
	// The control-plane stores this result as ResourceManager. Keep it a true
	// nil interface so application reconciliation cannot call a nil receiver.
	var runtime ResourceManager = manager
	if runtime != nil {
		t.Fatal("NewInClusterManager without SA token must remain nil as ResourceManager")
	}
}

func TestNewInClusterManagerOutsideCluster(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	manager, err := NewInClusterManager()
	if err != nil {
		t.Fatalf("NewInClusterManager outside cluster = %v, want nil error", err)
	}
	if manager != nil {
		t.Fatal("NewInClusterManager outside cluster should return a nil manager")
	}
}
