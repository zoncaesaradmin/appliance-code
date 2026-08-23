package applications

import (
	"os"
	"testing"
)

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
