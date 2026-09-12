package appliance_test

import (
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
)

func TestDevelopmentModuleCatalogProvidesReviewedRoutes(t *testing.T) {
	modules, err := appliance.DevelopmentModuleCatalog()
	if err != nil {
		t.Fatalf("DevelopmentModuleCatalog: %v", err)
	}
	host, ok := appliance.ModuleNamed(modules, appliance.ModuleNameHostAgent)
	if !ok || len(host.Routes) == 0 {
		t.Fatalf("host module = %+v, want reviewed routes", host)
	}
}
