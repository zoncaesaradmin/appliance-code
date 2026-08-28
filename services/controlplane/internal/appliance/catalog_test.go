package appliance_test

import (
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
)

func TestEmbeddedModuleCatalogProvidesReviewedRoutes(t *testing.T) {
	modules, err := appliance.EmbeddedModuleCatalog()
	if err != nil {
		t.Fatalf("EmbeddedModuleCatalog: %v", err)
	}
	host, ok := appliance.ModuleNamed(modules, appliance.ModuleNameHostAgent)
	if !ok || len(host.Routes) == 0 {
		t.Fatalf("host module = %+v, want reviewed routes", host)
	}
}
