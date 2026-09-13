package metadatabundle_test

import (
	"testing"

	"appliance-code/services/controlplane/internal/metadatabundle"
)

func TestDeliveryPackageMappings(t *testing.T) {
	b, err := metadatabundle.LoadDevelopment()
	if err != nil {
		t.Fatal(err)
	}
	for capability, want := range map[string]string{
		"artifact": "storage-network", "dns": "storage-network",
		"workflows": "build-workflows", "build": "build-workflows",
	} {
		got := b.Capabilities.Capabilities[capability].Packages
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s delivery packages = %v, want [%s]", capability, got, want)
		}
	}
}
