package metadatabundle_test

import (
	"reflect"
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
	if len(owners["inference"]) != 3 {
		t.Fatalf("inference owners = %q, want standard amd64 and accelerated amd64/arm64 packages", owners["inference"])
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
	delete(b.Packages.Packages, "acc-llm-amd64")
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
	if b.Packages.Packages["std-llm-amd64"].Runtime.InferenceEngine != "ollama" || b.Packages.Packages["std-llm-amd64"].Runtime.Architecture != "amd64" || !reflect.DeepEqual(b.Packages.Packages["std-llm-amd64"].Runtime.SupportedModes, []string{"cpu"}) {
		t.Fatalf("standard inference runtime = %+v", b.Packages.Packages["std-llm-amd64"].Runtime)
	}
	if !reflect.DeepEqual(b.Packages.Packages["acc-llm-arm64"].Runtime.SupportedModes, []string{"cpu", "cuda"}) {
		t.Fatalf("accelerated inference runtime = %+v", b.Packages.Packages["acc-llm-arm64"].Runtime)
	}
	if runtime := b.Packages.Packages["acc-llm-amd64"].Runtime; runtime.InferenceEngine != "vllm" || runtime.Architecture != "amd64" || !reflect.DeepEqual(runtime.SupportedModes, []string{"cpu"}) {
		t.Fatalf("accelerated amd64 inference runtime = %+v", runtime)
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
				pkg := b.Packages.Packages["std-llm-amd64"]
				pkg.Runtime.InferenceEngine = ""
				b.Packages.Packages["std-llm-amd64"] = pkg
			}
			if err := metadatabundle.ValidateBundle(b); err == nil {
				t.Fatal("invalid runtime catalog accepted")
			}
		})
	}
}
