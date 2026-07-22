package chart

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func requireHelm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed on PATH; skipping chart tests")
	}
}

func chartDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed to resolve this file's path")
	}
	return filepath.Dir(file)
}

func renderChart(t *testing.T, extraArgs ...string) []map[string]any {
	t.Helper()
	requireHelm(t)

	args := append([]string{"template", "argo-workflows", chartDir(t), "--namespace", "workflows"}, extraArgs...)
	cmd := exec.Command("helm", args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("helm template failed: %v\n%s", err, errOut.String())
	}

	var docs []map[string]any
	dec := yaml.NewDecoder(bytes.NewReader(out.Bytes()))
	for {
		var doc map[string]any
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decoding rendered YAML: %v", err)
		}
		if doc == nil {
			continue
		}
		docs = append(docs, doc)
	}
	return docs
}

func findByKindAndName(docs []map[string]any, kind, name string) map[string]any {
	for _, d := range docs {
		if k, _ := d["kind"].(string); k != kind {
			continue
		}
		if n, _ := at(d, "metadata", "name").(string); n == name {
			return d
		}
	}
	return nil
}

func at(doc map[string]any, path ...string) any {
	var cur any = doc
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func TestHelmLint(t *testing.T) {
	requireHelm(t)
	out, err := exec.Command("helm", "lint", chartDir(t)).CombinedOutput()
	if err != nil {
		t.Fatalf("helm lint failed: %v\n%s", err, out)
	}
}

func TestWorkflowControllerDoesNotUseExternalHelperImages(t *testing.T) {
	docs := renderChart(t)
	dep := findByKindAndName(docs, "Deployment", "argo-workflows")
	if dep == nil {
		t.Fatal("expected workflow-controller Deployment")
	}
	if initContainers, _ := at(dep, "spec", "template", "spec", "initContainers").([]any); len(initContainers) != 0 {
		t.Fatalf("expected no initContainers, got %v", initContainers)
	}
	volumes, _ := at(dep, "spec", "template", "spec", "volumes").([]any)
	for _, raw := range volumes {
		volume, _ := raw.(map[string]any)
		if name, _ := volume["name"].(string); name == "appliance-logs" {
			if path, _ := at(volume, "hostPath", "path").(string); path != "/data/zon/logs" {
				t.Fatalf("appliance-logs hostPath = %q, want /data/zon/logs", path)
			}
			return
		}
	}
	t.Fatal("expected appliance-logs hostPath volume")
}
