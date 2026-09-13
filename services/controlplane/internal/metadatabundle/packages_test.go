package metadatabundle_test

import (
	"testing"

	"appliance-code/services/controlplane/internal/metadatabundle"
)

func TestDeliveryPackageCatalogMapsEveryCapabilityOnce(t *testing.T) {
	b, err := metadatabundle.LoadDevelopment()
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string]string{}
	for packageID, pkg := range b.Packages.Packages {
		for _, capability := range pkg.Capabilities {
			if owner := owners[capability]; owner != "" {
				t.Fatalf("%s is owned by %s and %s", capability, owner, packageID)
			}
			owners[capability] = packageID
		}
	}
	for capability := range b.Capabilities.Capabilities {
		if owners[capability] == "" {
			t.Fatalf("%s has no delivery package", capability)
		}
	}
	if owners["artifact"] != "dev-platform" || owners["dns"] != "dev-platform" || owners["workflows"] != "dev-platform" || owners["build"] != "dev-platform" {
		t.Fatalf("dev-platform ownership = artifact:%s dns:%s workflows:%s build:%s", owners["artifact"], owners["dns"], owners["workflows"], owners["build"])
	}
}

func TestDeliveryPackageCatalogRejectsIncompleteMapping(t *testing.T) {
	b, err := metadatabundle.LoadDevelopment()
	if err != nil {
		t.Fatal(err)
	}
	delete(b.Packages.Packages, "inference")
	if err := metadatabundle.ValidateBundle(b); err == nil {
		t.Fatal("catalog without an inference package was accepted")
	}
}
