package metadatabundle_test

import (
	"testing"

	"appliance-code/services/controlplane/internal/metadatabundle"
)

func TestCurrentDeliveryPackagesCoverEveryCapability(t *testing.T) {
	b, err := metadatabundle.LoadDevelopment()
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string][]string{}
	for packageID, pkg := range b.Packages.Packages {
		for _, capability := range pkg.Capabilities {
			owners[capability] = append(owners[capability], packageID)
		}
	}
	for capability := range b.Capabilities.Capabilities {
		if len(owners[capability]) == 0 {
			t.Fatalf("%s has no delivery package", capability)
		}
	}
	if len(owners["inference"]) != 2 {
		t.Fatalf("inference owners = %q, want standard and accelerated packages", owners["inference"])
	}
	for _, packageID := range []string{"inference", "legacy-inference", "legacy-gpu"} {
		if _, ok := b.Packages.Packages[packageID]; ok {
			t.Fatalf("unexpected package %q", packageID)
		}
	}
	if owners["artifact"][0] != "dev-platform" || owners["dns"][0] != "dev-platform" || owners["workflows"][0] != "dev-platform" || owners["build"][0] != "dev-platform" {
		t.Fatalf("dev-platform ownership = artifact:%s dns:%s workflows:%s build:%s", owners["artifact"], owners["dns"], owners["workflows"], owners["build"])
	}
}

func TestDeliveryPackageCatalogRejectsIncompleteMapping(t *testing.T) {
	b, err := metadatabundle.LoadDevelopment()
	if err != nil {
		t.Fatal(err)
	}
	delete(b.Packages.Packages, "std-llm-amd64")
	delete(b.Packages.Packages, "acc-llm-arm64")
	if err := metadatabundle.ValidateBundle(b); err == nil {
		t.Fatal("catalog without a standard inference package was accepted")
	}
}

func TestInferencePackageDeclaresItsEngine(t *testing.T) {
	b, err := metadatabundle.LoadDevelopment()
	if err != nil {
		t.Fatal(err)
	}
	if err := metadatabundle.ValidateBundle(b); err != nil {
		t.Fatal(err)
	}
	cpu := b.Packages.Packages["std-llm-amd64"].Runtimes["inference"]
	if cpu.Engine != "ollama" {
		t.Fatalf("CPU runtime = %+v", cpu)
	}
}

func TestRuntimePackageCatalogRejectsAmbiguity(t *testing.T) {
	for _, invalid := range []string{"missing-engine"} {
		t.Run(invalid, func(t *testing.T) {
			b, err := metadatabundle.LoadDevelopment()
			if err != nil {
				t.Fatal(err)
			}
			switch invalid {
			case "missing-engine":
				b.Packages.Packages["std-llm-amd64"].Runtimes["inference"] = metadatabundle.PackageRuntime{}
			}
			if err := metadatabundle.ValidateBundle(b); err == nil {
				t.Fatal("invalid runtime catalog accepted")
			}
		})
	}
}
