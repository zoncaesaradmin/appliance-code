// Package chart holds structural policy tests for the
// appliance-control-plane Helm chart. These tests shell out to a locally
// installed `helm` to lint and render the chart, then assert the rendered
// manifests satisfy the plan's Kubernetes hardening requirements. They do
// not require a live cluster: rendering and static policy checks are all
// that's possible in this development environment, per the corrected
// Phase 0 scope note in docs/control-plane-v1-plan.md. Real install/
// restart/air-gap evidence is separate, cluster-dependent validation.
package chart

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

	args := append([]string{"template", "appliance", chartDir(t), "--namespace", "appliance"}, extraArgs...)
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

func findByKind(docs []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, d := range docs {
		if k, _ := d["kind"].(string); k == kind {
			out = append(out, d)
		}
	}
	return out
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

// at walks nested maps by key, returning nil if any step is missing or not
// a map, so callers can write a single assertion without a chain of ok
// checks.
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
	cmd := exec.Command("helm", "lint", chartDir(t))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm lint failed: %v\n%s", err, out)
	}
}

func defaultRenderArgs() []string {
	return []string{"--set", "networkPolicy.traefikNamespaceLabel.kubernetes\\.io/metadata\\.name=traefik"}
}

const (
	controlPlaneDeploymentName = "control-plane"
	controlPlaneConfigMapName  = "control-plane-config"
	controlPlaneServiceName    = "control-plane"
	controlPlaneUIName         = "control-plane-ui"
	controlPlaneUIConfigName   = "control-plane-ui-config"
)

func TestExactlyOneReplicaWithRecreateStrategy(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	dep := findByKindAndName(docs, "Deployment", controlPlaneDeploymentName)
	if dep == nil {
		t.Fatal("expected control-plane Deployment")
	}

	replicas, _ := at(dep, "spec", "replicas").(int)
	if replicas != 1 {
		t.Errorf("replicas = %v, want 1 (ADR 0004 fixes exactly one replica while SQLite is active)", at(dep, "spec", "replicas"))
	}
	if strategyType, _ := at(dep, "spec", "strategy", "type").(string); strategyType != "Recreate" {
		t.Errorf("strategy.type = %q, want Recreate (a rolling update would run two replicas against one SQLite file)", strategyType)
	}
}

func TestPodAndContainerSecurityHardening(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	dep := findByKindAndName(docs, "Deployment", controlPlaneDeploymentName)
	if dep == nil {
		t.Fatal("expected control-plane Deployment")
	}
	podSpec, ok := at(dep, "spec", "template", "spec").(map[string]any)
	if !ok {
		t.Fatal("could not find spec.template.spec on the Deployment")
	}

	if automount, _ := podSpec["automountServiceAccountToken"].(bool); automount {
		t.Error("automountServiceAccountToken should be false for core/default rendering")
	}

	podSecCtx, _ := podSpec["securityContext"].(map[string]any)
	if runAsNonRoot, _ := podSecCtx["runAsNonRoot"].(bool); !runAsNonRoot {
		t.Error("pod securityContext.runAsNonRoot should be true")
	}
	if runAsUser, _ := podSecCtx["runAsUser"].(int); runAsUser != 10001 {
		t.Errorf("pod securityContext.runAsUser = %d, want 10001", runAsUser)
	}
	if runAsGroup, _ := podSecCtx["runAsGroup"].(int); runAsGroup != 10001 {
		t.Errorf("pod securityContext.runAsGroup = %d, want 10001", runAsGroup)
	}
	if fsGroup, _ := podSecCtx["fsGroup"].(int); fsGroup != 20000 {
		t.Errorf("pod securityContext.fsGroup = %d, want 20000", fsGroup)
	}
	if policy, _ := podSecCtx["fsGroupChangePolicy"].(string); policy != "OnRootMismatch" {
		t.Errorf("pod securityContext.fsGroupChangePolicy = %q, want OnRootMismatch", policy)
	}
	seccomp, _ := podSecCtx["seccompProfile"].(map[string]any)
	if seccompType, _ := seccomp["type"].(string); seccompType != "RuntimeDefault" {
		t.Errorf("pod seccompProfile.type = %q, want RuntimeDefault", seccompType)
	}

	containers, _ := podSpec["containers"].([]any)
	if len(containers) != 1 {
		t.Fatalf("expected exactly one container, got %d", len(containers))
	}
	container, _ := containers[0].(map[string]any)

	containerSecCtx, _ := container["securityContext"].(map[string]any)
	if ro, _ := containerSecCtx["readOnlyRootFilesystem"].(bool); !ro {
		t.Error("container securityContext.readOnlyRootFilesystem should be true")
	}
	if allowEsc, _ := containerSecCtx["allowPrivilegeEscalation"].(bool); allowEsc {
		t.Error("container securityContext.allowPrivilegeEscalation should be false")
	}
	capabilities, _ := containerSecCtx["capabilities"].(map[string]any)
	dropped, _ := capabilities["drop"].([]any)
	if len(dropped) != 1 || dropped[0] != "ALL" {
		t.Errorf("container securityContext.capabilities.drop = %v, want [ALL]", dropped)
	}

	resources, _ := container["resources"].(map[string]any)
	if resources["requests"] == nil || resources["limits"] == nil {
		t.Error("container should declare both resource requests and limits")
	}

	for _, probeName := range []string{"livenessProbe", "readinessProbe", "startupProbe"} {
		if container[probeName] == nil {
			t.Errorf("container should declare %s", probeName)
		}
	}
}

func TestUIPodUsesDedicatedIdentityAndSharedFilesystemGroup(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	dep := findByKindAndName(docs, "Deployment", controlPlaneUIName)
	if dep == nil {
		t.Fatal("expected control-plane UI Deployment")
	}
	podSpec, ok := at(dep, "spec", "template", "spec").(map[string]any)
	if !ok {
		t.Fatal("could not find spec.template.spec on the UI Deployment")
	}
	podSecCtx, _ := podSpec["securityContext"].(map[string]any)
	if runAsNonRoot, _ := podSecCtx["runAsNonRoot"].(bool); !runAsNonRoot {
		t.Error("UI pod securityContext.runAsNonRoot should be true")
	}
	if runAsUser, _ := podSecCtx["runAsUser"].(int); runAsUser != 10002 {
		t.Errorf("UI pod securityContext.runAsUser = %d, want 10002", runAsUser)
	}
	if runAsGroup, _ := podSecCtx["runAsGroup"].(int); runAsGroup != 10002 {
		t.Errorf("UI pod securityContext.runAsGroup = %d, want 10002", runAsGroup)
	}
	if fsGroup, _ := podSecCtx["fsGroup"].(int); fsGroup != 20000 {
		t.Errorf("UI pod securityContext.fsGroup = %d, want 20000", fsGroup)
	}
	if policy, _ := podSecCtx["fsGroupChangePolicy"].(string); policy != "OnRootMismatch" {
		t.Errorf("UI pod securityContext.fsGroupChangePolicy = %q, want OnRootMismatch", policy)
	}
}

func TestServiceLogDirectoriesAreOperatorReadable(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	cases := []struct {
		name       string
		deployName string
		serviceDir string
		owner      string
	}{
		{
			name:       "control-plane",
			deployName: controlPlaneDeploymentName,
			serviceDir: "/data/zon/logs/control-plane",
			owner:      "10001:20000",
		},
		{
			name:       "ui",
			deployName: controlPlaneUIName,
			serviceDir: "/data/zon/logs/ui",
			owner:      "10002:20000",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dep := findByKindAndName(docs, "Deployment", tc.deployName)
			if dep == nil {
				t.Fatalf("expected Deployment %s", tc.deployName)
			}
			initContainers, _ := at(dep, "spec", "template", "spec", "initContainers").([]any)
			if len(initContainers) == 0 {
				t.Fatal("expected prepare-log-dir init container")
			}
			init, _ := initContainers[0].(map[string]any)
			command, _ := init["command"].([]any)
			if len(command) < 3 {
				t.Fatalf("prepare-log-dir command = %v", command)
			}
			script, _ := command[2].(string)
			for _, want := range []string{tc.serviceDir, "chown " + tc.owner, "chmod 2755"} {
				if !strings.Contains(script, want) {
					t.Fatalf("prepare-log-dir command = %q, want %q", script, want)
				}
			}
			if strings.Contains(script, "chmod 2770") {
				t.Fatalf("service log directory must be operator-readable, got command %q", script)
			}
		})
	}
}

func TestNetworkPolicyDefaultDenyPresent(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	policies := findByKind(docs, "NetworkPolicy")
	if len(policies) < 2 {
		t.Fatalf("expected at least a default-deny and an allow NetworkPolicy, got %d", len(policies))
	}

	var foundDefaultDeny bool
	for _, p := range policies {
		podSelector, _ := at(p, "spec", "podSelector").(map[string]any)
		policyTypes, _ := at(p, "spec", "policyTypes").([]any)
		if len(podSelector) == 0 && len(policyTypes) == 2 {
			foundDefaultDeny = true
		}
	}
	if !foundDefaultDeny {
		t.Error("expected one NetworkPolicy with an empty podSelector (applies to all pods) and both Ingress and Egress policyTypes")
	}
}

func TestIngressRouteOnlyReferencesPublicService(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	routes := findByKind(docs, "IngressRoute")
	if len(routes) != 1 {
		t.Fatalf("expected exactly one IngressRoute, got %d", len(routes))
	}

	services, _ := at(routes[0], "spec", "routes").([]any)
	if len(services) == 0 {
		t.Fatal("IngressRoute should declare at least one route")
	}
	for _, route := range services {
		routeMap, _ := route.(map[string]any)
		svcList, _ := routeMap["services"].([]any)
		for _, svc := range svcList {
			svcMap, _ := svc.(map[string]any)
			name, _ := svcMap["name"].(string)
			if name == "" {
				t.Error("IngressRoute service entry missing a name")
				continue
			}
			if len(name) >= len("-internal") && name[len(name)-len("-internal"):] == "-internal" {
				t.Errorf("IngressRoute must never reference the internal service, found %q", name)
			}
		}
	}
}

func TestUIResourcesRenderByDefault(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	if findByKindAndName(docs, "Deployment", controlPlaneUIName) == nil {
		t.Fatal("expected UI Deployment")
	}
	if findByKindAndName(docs, "Service", controlPlaneUIName) == nil {
		t.Fatal("expected UI Service")
	}
	if findByKindAndName(docs, "ConfigMap", controlPlaneUIConfigName) == nil {
		t.Fatal("expected UI ConfigMap")
	}
	if findByKindAndName(docs, "NetworkPolicy", controlPlaneUIName+"-allow") == nil {
		t.Fatal("expected UI NetworkPolicy")
	}
}

func TestUIConfigMapDefaultsToRenderedControlPlaneServiceNames(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneUIConfigName)
	if cm == nil {
		t.Fatal("expected UI ConfigMap")
	}

	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_CONTROL_PLANE_BASE_URL"].(string); got != "http://control-plane:8080" {
		t.Fatalf("APPLIANCE_CONTROL_PLANE_BASE_URL = %q, want http://control-plane:8080", got)
	}
	if got, _ := data["APPLIANCE_CONTROL_PLANE_INTERNAL_BASE_URL"].(string); got != "http://control-plane-internal:8081" {
		t.Fatalf("APPLIANCE_CONTROL_PLANE_INTERNAL_BASE_URL = %q, want http://control-plane-internal:8081", got)
	}
}

func TestIngressRoutesAPIToControlPlaneAndRootToUI(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	routes := findByKind(docs, "IngressRoute")
	if len(routes) != 1 {
		t.Fatalf("expected exactly one IngressRoute, got %d", len(routes))
	}
	routeList, _ := at(routes[0], "spec", "routes").([]any)
	if len(routeList) != 2 {
		t.Fatalf("expected API and UI routes, got %d", len(routeList))
	}

	var apiRouteOK, uiRouteOK bool
	for _, raw := range routeList {
		route, _ := raw.(map[string]any)
		match, _ := route["match"].(string)
		services, _ := route["services"].([]any)
		if len(services) != 1 {
			continue
		}
		svc, _ := services[0].(map[string]any)
		name, _ := svc["name"].(string)
		switch {
		case match == "(PathPrefix(`/api/v1`) || PathPrefix(`/mcp`))" && name == controlPlaneServiceName:
			apiRouteOK = true
		case match == "PathPrefix(`/`)" && name == controlPlaneUIName:
			uiRouteOK = true
		}
	}
	if !apiRouteOK {
		t.Error("expected /api/v1 and /mcp route to target control-plane service")
	}
	if !uiRouteOK {
		t.Error("expected / route to target UI service")
	}
}

func TestDisablingOptionalFeaturesRendersCleanly(t *testing.T) {
	docs := renderChart(t, "--set", "namespace.create=false", "--set", "persistence.enabled=false", "--set", "ingress.enabled=false", "--set", "ui.enabled=false")
	if len(findByKind(docs, "Namespace")) != 0 {
		t.Error("namespace.create=false should omit the Namespace object")
	}
	if len(findByKind(docs, "PersistentVolumeClaim")) != 0 {
		t.Error("persistence.enabled=false should omit the PersistentVolumeClaim")
	}
	if len(findByKind(docs, "IngressRoute")) != 0 {
		t.Error("ingress.enabled=false should omit the IngressRoute")
	}
	// The Deployment must still render without a dangling volume/mount
	// reference to the now-absent PVC.
	if findByKindAndName(docs, "Deployment", controlPlaneDeploymentName) == nil {
		t.Error("control-plane Deployment should still render with persistence disabled")
	}
	if findByKindAndName(docs, "Deployment", controlPlaneUIName) != nil {
		t.Error("ui.enabled=false should omit the UI Deployment")
	}
}

func TestBuildCatalogRendersAsControlPlaneConfig(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "config.applianceProfile=builder",
		"--set", "config.buildCatalog.workProfiles[0].name=builder",
		"--set", "config.buildCatalog.workProfiles[0].repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].url=https://git.internal.example.com/team/app.git",
		"--set", "config.workspaceProvisionerImageDigest=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)...)
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_PROFILE"].(string); got != "builder" {
		t.Fatalf("APPLIANCE_PROFILE = %q, want builder", got)
	}
	catalogJSON, _ := data["APPLIANCE_BUILD_CATALOG_JSON"].(string)
	if catalogJSON == "" || !bytes.Contains([]byte(catalogJSON), []byte("workProfiles")) {
		t.Fatalf("APPLIANCE_BUILD_CATALOG_JSON = %q, want rendered catalog", catalogJSON)
	}
	if got, _ := data["APPLIANCE_WORKSPACE_PROVISIONER_IMAGE_DIGEST"].(string); got != "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("APPLIANCE_WORKSPACE_PROVISIONER_IMAGE_DIGEST = %q, want rendered provisioner image", got)
	}
}

func TestValuesSchemaRejectsUnsafeBuildCatalogPath(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "bad-values.yaml")
	values := []byte(`
config:
  applianceProfile: builder
  workspaceProvisionerImageDigest: workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  buildCatalog:
    workProfiles:
      - name: builder
        repos:
          - name: app
    repos:
      - name: app
        url: https://git.internal.example.com/team/app.git
    buildTargets:
      - name: default
        repo: app
        execution: repo_script
        scriptPath: ../build.sh
        imageRepository: users/alice/app
        builderImageDigest: buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("writing test values: %v", err)
	}
	cmd := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helm lint unexpectedly accepted unsafe build catalog path\n%s", out)
	}
	if !bytes.Contains(out, []byte("buildCatalog")) && !bytes.Contains(out, []byte("scriptPath")) {
		t.Fatalf("helm lint failed for the wrong reason; output:\n%s", out)
	}
}

func TestValuesSchemaRejectsBuilderWithoutBuildCatalog(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "bad-builder-catalog-required.yaml")
	values := []byte(`
config:
  applianceProfile: builder
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("writing test values: %v", err)
	}
	cmd := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helm lint unexpectedly accepted builder config without buildCatalog\n%s", out)
	}
	if !bytes.Contains(out, []byte("buildCatalog")) {
		t.Fatalf("helm lint failed for the wrong reason; output:\n%s", out)
	}
}

func TestValuesSchemaRejectsBuilderWithoutWorkspaceProvisionerImage(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "bad-builder-catalog.yaml")
	values := []byte(`
config:
  applianceProfile: builder
  buildCatalog:
    workProfiles:
      - name: builder
        repos:
          - name: app
    repos:
      - name: app
        url: https://git.internal.example.com/team/app.git
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("writing test values: %v", err)
	}
	cmd := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helm lint unexpectedly accepted builder config without workspaceProvisionerImageDigest\n%s", out)
	}
	if !bytes.Contains(out, []byte("workspaceProvisionerImageDigest")) {
		t.Fatalf("helm lint failed for the wrong reason; output:\n%s", out)
	}
}

func TestValuesSchemaRejectsSSHCatalogRepo(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "ssh-catalog.yaml")
	values := []byte(`
config:
  applianceProfile: builder
  workspaceProvisionerImageDigest: workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  buildCatalog:
    workProfiles:
      - name: builder
        repos:
          - name: app
    repos:
      - name: app
        url: git@git.internal.example.com:team/app.git
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("writing test values: %v", err)
	}
	cmd := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("helm lint unexpectedly accepted SSH catalog repo\n%s", out)
	}
}

func TestBuilderWorkspacePVCAndConfigRender(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "config.applianceProfile=builder",
		"--set", "config.buildCatalog.workProfiles[0].name=builder",
		"--set", "config.buildCatalog.workProfiles[0].repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].url=https://git.internal.example.com/team/app.git",
		"--set", "config.workspaceProvisionerImageDigest=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)...)
	pv := findByKindAndName(docs, "PersistentVolume", controlPlaneDeploymentName+"-workspaces")
	if pv == nil {
		t.Fatal("expected builder workspace PV")
	}
	if hostPath, _ := at(pv, "spec", "hostPath", "path").(string); hostPath != "/data/zon/workspaces" {
		t.Fatalf("workspace PV hostPath = %q, want /data/zon/workspaces", hostPath)
	}
	pvc := findByKindAndName(docs, "PersistentVolumeClaim", controlPlaneDeploymentName+"-workspaces")
	if pvc == nil {
		t.Fatal("expected builder workspace PVC")
	}
	if ns, _ := at(pvc, "metadata", "namespace").(string); ns != "appliance-builds" {
		t.Fatalf("workspace PVC namespace = %q, want appliance-builds", ns)
	}
	if volumeName, _ := at(pvc, "spec", "volumeName").(string); volumeName != "control-plane-workspaces" {
		t.Fatalf("workspace PVC volumeName = %q, want control-plane-workspaces", volumeName)
	}
	jobs := findByKind(docs, "Job")
	var prepJob map[string]any
	for _, job := range jobs {
		name, _ := at(job, "metadata", "name").(string)
		if strings.HasPrefix(name, "control-plane-workspace-storage-prep-") {
			prepJob = job
			break
		}
	}
	if prepJob == nil {
		t.Fatal("expected builder workspace storage prep Job")
	}
	if ns, _ := at(prepJob, "metadata", "namespace").(string); ns != "appliance-builds" {
		t.Fatalf("workspace storage prep Job namespace = %q, want appliance-builds", ns)
	}
	podSpec, _ := at(prepJob, "spec", "template", "spec").(map[string]any)
	containers, _ := podSpec["containers"].([]any)
	container, _ := containers[0].(map[string]any)
	secCtx, _ := container["securityContext"].(map[string]any)
	if runAsUser, _ := secCtx["runAsUser"].(int); runAsUser != 0 {
		t.Fatalf("workspace storage prep Job runAsUser = %d, want 0", runAsUser)
	}
	command, _ := container["command"].([]any)
	if len(command) < 3 || !strings.Contains(command[2].(string), "chown 0:20000") || !strings.Contains(command[2].(string), "chmod 2770") {
		t.Fatalf("workspace storage prep command = %v, want shared GID ownership and setgid mode", command)
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_WORKSPACE_ROOT_DIR"].(string); got != "/data/zon/workspaces" {
		t.Fatalf("APPLIANCE_WORKSPACE_ROOT_DIR = %q, want /data/zon/workspaces", got)
	}
	if got, _ := data["APPLIANCE_WORKSPACE_CLAIM_NAME"].(string); got != "control-plane-workspaces" {
		t.Fatalf("APPLIANCE_WORKSPACE_CLAIM_NAME = %q, want control-plane-workspaces", got)
	}
	if got, _ := data["APPLIANCE_WORKFLOW_INSTANCE_ID"].(string); got != "appliance" {
		t.Fatalf("APPLIANCE_WORKFLOW_INSTANCE_ID = %q, want appliance", got)
	}
	if got, _ := data["APPLIANCE_WORKFLOW_EXECUTOR_SERVICE_ACCOUNT"].(string); got != "appliance-argo-workflows-executor" {
		t.Fatalf("APPLIANCE_WORKFLOW_EXECUTOR_SERVICE_ACCOUNT = %q, want appliance-argo-workflows-executor", got)
	}
	if got, _ := data["APPLIANCE_WORKSPACE_PROVISIONER_IMAGE_DIGEST"].(string); got != "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("APPLIANCE_WORKSPACE_PROVISIONER_IMAGE_DIGEST = %q, want workspace provisioner image", got)
	}
}

func TestBuilderArgoWorkflowRBACRenders(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "config.applianceProfile=builder",
		"--set", "config.buildCatalog.workProfiles[0].name=builder",
		"--set", "config.buildCatalog.workProfiles[0].repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].url=https://git.internal.example.com/team/app.git",
		"--set", "config.workspaceProvisionerImageDigest=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)...)
	dep := findByKindAndName(docs, "Deployment", controlPlaneDeploymentName)
	if dep == nil {
		t.Fatal("expected control-plane Deployment")
	}
	if automount, _ := at(dep, "spec", "template", "spec", "automountServiceAccountToken").(bool); !automount {
		t.Fatal("builder/argo deployment should mount a service account token")
	}
	role := findByKindAndName(docs, "Role", controlPlaneDeploymentName+"-workflows")
	if role == nil {
		t.Fatal("expected workflow Role for builder/argo")
	}
	if ns, _ := at(role, "metadata", "namespace").(string); ns != "appliance-builds" {
		t.Fatalf("workflow Role namespace = %q, want appliance-builds", ns)
	}
	rules, _ := at(role, "rules").([]any)
	if !roleRuleAllowsResource(rules, "secrets", "create", "get", "update") {
		t.Fatal("workflow Role should allow create/get/update on secrets for builder Git access")
	}
	if rb := findByKindAndName(docs, "RoleBinding", controlPlaneDeploymentName+"-workflows"); rb == nil {
		t.Fatal("expected workflow RoleBinding for builder/argo")
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if _, ok := data["APPLIANCE_ARGO_WORKFLOW_NAMESPACE"]; ok {
		t.Fatal("control-plane ConfigMap should not expose APPLIANCE_ARGO_WORKFLOW_NAMESPACE once the namespace is fixed in code")
	}
}

func roleRuleAllowsResource(rules []any, resource string, verbs ...string) bool {
	need := map[string]struct{}{}
	for _, verb := range verbs {
		need[verb] = struct{}{}
	}
	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			continue
		}
		resources, _ := rule["resources"].([]any)
		if !containsString(resources, resource) {
			continue
		}
		ruleVerbs, _ := rule["verbs"].([]any)
		missing := false
		for verb := range need {
			if !containsString(ruleVerbs, verb) {
				missing = true
				break
			}
		}
		if !missing {
			return true
		}
	}
	return false
}

func containsString(values []any, want string) bool {
	for _, value := range values {
		if got, _ := value.(string); got == want {
			return true
		}
	}
	return false
}
