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
	"fmt"
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
	controlPlaneDeploymentName = "controlplane"
	controlPlaneConfigMapName  = "controlplane-config"
	controlPlaneServiceName    = "controlplane"
	automationRuntimeName      = "automation-runtime"
	automationRuntimeConfig    = "automation-runtime-config"
	controlPlaneUIName         = "ui-server"
	controlPlaneUIConfigName   = "ui-server-config"
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
		t.Error("automountServiceAccountToken should be false when Application Management is not enabled by the signed profile")
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
	}{
		{
			name:       "controlplane",
			deployName: controlPlaneDeploymentName,
		},
		{
			name:       "ui",
			deployName: controlPlaneUIName,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dep := findByKindAndName(docs, "Deployment", tc.deployName)
			if dep == nil {
				t.Fatalf("expected Deployment %s", tc.deployName)
			}
			if initContainers, _ := at(dep, "spec", "template", "spec", "initContainers").([]any); len(initContainers) != 0 {
				t.Fatalf("expected no installer-bypassing init containers, got %v", initContainers)
			}
			volumes, _ := at(dep, "spec", "template", "spec", "volumes").([]any)
			var sawLogsVolume bool
			for _, raw := range volumes {
				volume, _ := raw.(map[string]any)
				if name, _ := volume["name"].(string); name == "appliance-logs" {
					if path, _ := at(volume, "hostPath", "path").(string); path != "/data/zon/logs" {
						t.Fatalf("appliance-logs hostPath = %q, want /data/zon/logs", path)
					}
					sawLogsVolume = true
				}
			}
			if !sawLogsVolume {
				t.Fatal("expected appliance-logs hostPath volume")
			}
		})
	}
}

func TestKeySecretVolumeIsReadableByNonRootControlPlane(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	dep := findByKindAndName(docs, "Deployment", controlPlaneDeploymentName)
	if dep == nil {
		t.Fatal("expected control-plane Deployment")
	}
	volumes, _ := at(dep, "spec", "template", "spec", "volumes").([]any)
	for _, raw := range volumes {
		volume, _ := raw.(map[string]any)
		if name, _ := volume["name"].(string); name != "keys" {
			continue
		}
		mode, _ := at(volume, "secret", "defaultMode").(int)
		if mode != 0o440 {
			t.Fatalf("keys secret defaultMode = %#o, want 0440 so the non-root control-plane can read mounted keys", mode)
		}
		return
	}
	t.Fatal("expected keys secret volume on control-plane Deployment")
}

func TestRenderedChartContainsNoExternalHelperImages(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "config.applianceProfile=builder",
		"--set", "config.buildCatalog.workProfiles[0].name=builder",
		"--set", "config.buildCatalog.workProfiles[0].repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].url=https://git.internal.example.com/team/app.git",
		"--set", "config.workspaceProvisionerImageDigest=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"--set", "config.builderImageDigest=buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)...)
	rendered, err := yaml.Marshal(docs)
	if err != nil {
		t.Fatalf("marshal rendered manifests: %v", err)
	}
	for _, forbidden := range []string{"alpine:3.24.1", "prepare-log-dir"} {
		if bytes.Contains(rendered, []byte(forbidden)) {
			t.Fatalf("rendered manifests must not contain %q:\n%s", forbidden, rendered)
		}
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

func TestAutomationRuntimeResourcesRenderByDefault(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	if findByKindAndName(docs, "Deployment", automationRuntimeName) == nil {
		t.Fatal("expected automation runtime Deployment")
	}
	if findByKindAndName(docs, "Service", automationRuntimeName) == nil {
		t.Fatal("expected automation runtime Service")
	}
	if findByKindAndName(docs, "ConfigMap", automationRuntimeConfig) == nil {
		t.Fatal("expected automation runtime ConfigMap")
	}
	if findByKindAndName(docs, "NetworkPolicy", automationRuntimeName+"-allow") == nil {
		t.Fatal("expected automation runtime NetworkPolicy")
	}
	if findByKindAndName(docs, "PersistentVolumeClaim", automationRuntimeName+"-data") == nil {
		t.Fatal("expected automation runtime PVC")
	}
	dep := findByKindAndName(docs, "Deployment", automationRuntimeName)
	sec, _ := at(dep, "spec", "template", "spec", "securityContext").(map[string]any)
	if runAsUser, _ := sec["runAsUser"].(int); runAsUser != 10007 {
		t.Fatalf("automation runtime runAsUser = %d, want 10007 (must not reuse inference 10006)", runAsUser)
	}
	volumes, _ := at(dep, "spec", "template", "spec", "volumes").([]any)
	var sawOwnPVC bool
	for _, raw := range volumes {
		volume, _ := raw.(map[string]any)
		if name, _ := volume["name"].(string); name != "data" {
			continue
		}
		claim, _ := at(volume, "persistentVolumeClaim", "claimName").(string)
		if claim == automationRuntimeName+"-data" {
			sawOwnPVC = true
		}
		if claim == controlPlaneDeploymentName+"-data" {
			t.Fatal("automation runtime must not mount the control-plane data PVC (single-writer SQLite)")
		}
	}
	if !sawOwnPVC {
		t.Fatal("expected automation runtime to mount its own data PVC")
	}
}

func TestControlPlaneConfigPointsToAutomationRuntimeService(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_AUTOMATION_RUNTIME_BASE_URL"].(string); got != "http://automation-runtime.appliance.svc.cluster.local:8082" {
		t.Fatalf("APPLIANCE_AUTOMATION_RUNTIME_BASE_URL = %q, want http://automation-runtime.appliance.svc.cluster.local:8082", got)
	}
}

func TestUIConfigMapDefaultsToRenderedControlPlaneServiceNames(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneUIConfigName)
	if cm == nil {
		t.Fatal("expected UI ConfigMap")
	}

	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_CONTROL_PLANE_BASE_URL"].(string); got != "http://controlplane.appliance.svc.cluster.local:8080" {
		t.Fatalf("APPLIANCE_CONTROL_PLANE_BASE_URL = %q, want http://controlplane.appliance.svc.cluster.local:8080", got)
	}
	if got, _ := data["APPLIANCE_CONTROL_PLANE_INTERNAL_BASE_URL"].(string); got != "http://controlplane-internal.appliance.svc.cluster.local:8081" {
		t.Fatalf("APPLIANCE_CONTROL_PLANE_INTERNAL_BASE_URL = %q, want http://controlplane-internal.appliance.svc.cluster.local:8081", got)
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
		priority, _ := route["priority"].(int)
		switch {
		case match == "(PathPrefix(`/api/v1`) || PathPrefix(`/mcp`) || PathPrefix(`/inference/v1`) || PathPrefix(`/video/v1`))" && name == controlPlaneServiceName:
			if priority != 100 {
				t.Errorf("API route priority = %v, want 100", route["priority"])
			}
			apiRouteOK = true
		case match == "PathPrefix(`/`) && !PathPrefix(`/api`) && !PathPrefix(`/mcp`) && !PathPrefix(`/inference`) && !PathPrefix(`/video`) && !PathPrefix(`/v2`)" && name == controlPlaneUIName:
			if priority != 1 {
				t.Errorf("UI route priority = %v, want 1", route["priority"])
			}
			uiRouteOK = true
		}
	}
	if !apiRouteOK {
		t.Error("expected /api/v1, /mcp, /inference/v1, and /video/v1 route to target control-plane service")
	}
	if !uiRouteOK {
		t.Error("expected / route to target UI service with API/MCP/inference/video/registry exclusions")
	}
}

func TestFilesMaxUploadBytesRendersAsDecimalString(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	cms := findByKind(docs, "ConfigMap")
	var found bool
	var foundPrefix bool
	for _, doc := range cms {
		data, _ := at(doc, "data").(map[string]any)
		if data == nil {
			continue
		}
		if raw, ok := data["APPLIANCE_FILES_OBJECT_PREFIX"].(string); ok {
			foundPrefix = true
			if raw != "files" {
				t.Fatalf("APPLIANCE_FILES_OBJECT_PREFIX = %q, want files", raw)
			}
		}
		raw, ok := data["APPLIANCE_FILES_MAX_UPLOAD_BYTES"].(string)
		if !ok {
			continue
		}
		found = true
		if raw != "21474836480" {
			t.Fatalf("APPLIANCE_FILES_MAX_UPLOAD_BYTES = %q, want decimal 21474836480 (not scientific notation)", raw)
		}
		if strings.Contains(strings.ToLower(raw), "e+") || strings.Contains(strings.ToLower(raw), "e-") {
			t.Fatalf("APPLIANCE_FILES_MAX_UPLOAD_BYTES must not use scientific notation, got %q", raw)
		}
	}
	if !found {
		t.Fatal("expected APPLIANCE_FILES_MAX_UPLOAD_BYTES in control-plane ConfigMap")
	}
	if !foundPrefix {
		t.Fatal("expected APPLIANCE_FILES_OBJECT_PREFIX in control-plane ConfigMap")
	}
}

func TestDisablingOptionalFeaturesRendersCleanly(t *testing.T) {
	docs := renderChart(t, "--set", "namespace.create=false", "--set", "persistence.enabled=false", "--set", "automationRuntime.persistence.enabled=false", "--set", "ingress.enabled=false", "--set", "ui.enabled=false")
	if findByKindAndName(docs, "Namespace", "ace-system") != nil {
		t.Error("namespace.create=false should omit the control-plane Namespace object")
	}
	if findByKindAndName(docs, "Namespace", "apps") != nil {
		t.Error("application namespace must be provisioned by the installer, not rendered by Helm")
	}
	// Control-plane and automation-runtime PVCs honor persistence.enabled.
	// Foundation blob-storage always renders its own claim (hostPath-backed).
	if findByKindAndName(docs, "PersistentVolumeClaim", controlPlaneDeploymentName+"-data") != nil {
		t.Error("persistence.enabled=false should omit the control-plane PersistentVolumeClaim")
	}
	if findByKindAndName(docs, "PersistentVolumeClaim", automationRuntimeName+"-data") != nil {
		t.Error("automationRuntime.persistence.enabled=false should omit the automation-runtime PersistentVolumeClaim")
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

func TestBlobStorageBelongsToAceSystemRolloutOnly(t *testing.T) {
	// zonctl installs the same chart as multiple Helm releases. Blob-storage
	// must render only for the ace-system release so appliance-ace-apps does
	// not try to own the Deployment.
	aceAppsDocs := renderChart(t, append(defaultRenderArgs(),
		"--set", "rollout.aceSystem.enabled=false",
		"--set", "rollout.aceApps.enabled=true",
		"--set", "rollout.applicationSupport.enabled=false",
		"--set", "rollout.dnsSupport.enabled=false",
		"--set", "rollout.workflowsSupport.enabled=false",
	)...)
	if findByKindAndName(aceAppsDocs, "Deployment", "blob-storage") != nil {
		t.Fatal("ace-apps rollout must not render blob-storage Deployment")
	}
	if findByKindAndName(aceAppsDocs, "Service", "blob-storage") != nil {
		t.Fatal("ace-apps rollout must not render blob-storage Service")
	}

	aceSystemDocs := renderChart(t, append(defaultRenderArgs(),
		"--set", "rollout.aceSystem.enabled=true",
		"--set", "rollout.aceApps.enabled=false",
		"--set", "rollout.applicationSupport.enabled=false",
		"--set", "rollout.dnsSupport.enabled=false",
		"--set", "rollout.workflowsSupport.enabled=false",
	)...)
	if findByKindAndName(aceSystemDocs, "Namespace", "blob-storage") != nil {
		t.Fatal("blob-storage must live in ace-system, not a dedicated Namespace object")
	}
	blobDeploy := findByKindAndName(aceSystemDocs, "Deployment", "blob-storage")
	if blobDeploy == nil {
		t.Fatal("ace-system rollout must render blob-storage Deployment")
	}
	cpDeploy := findByKindAndName(aceSystemDocs, "Deployment", controlPlaneDeploymentName)
	if cpDeploy == nil {
		t.Fatal("expected control-plane Deployment")
	}
	blobNS, _ := at(blobDeploy, "metadata", "namespace").(string)
	cpNS, _ := at(cpDeploy, "metadata", "namespace").(string)
	if blobNS == "" || blobNS != cpNS {
		t.Fatalf("blob-storage Deployment namespace = %q, want same as controlplane %q", blobNS, cpNS)
	}
	if initContainers, _ := at(blobDeploy, "spec", "template", "spec", "initContainers").([]any); len(initContainers) != 0 {
		t.Fatalf("blob-storage must not use a root init container under restricted PSA, got %v", initContainers)
	}
	blobSvc := findByKindAndName(aceSystemDocs, "Service", "blob-storage")
	if blobSvc == nil {
		t.Fatal("ace-system rollout must render blob-storage Service")
	}
	if ns, _ := at(blobSvc, "metadata", "namespace").(string); ns != blobNS {
		t.Fatalf("blob-storage Service namespace = %q, want %q", ns, blobNS)
	}
	if findByKindAndName(aceSystemDocs, "NetworkPolicy", "blob-storage-allow") == nil {
		t.Fatal("expected pod-scoped blob-storage-allow NetworkPolicy")
	}
	if findByKindAndName(aceSystemDocs, "NetworkPolicy", "blob-storage-default-deny") != nil {
		t.Fatal("blob-storage must not add a namespace-wide default-deny in ace-system")
	}
	cpAllow := findByKindAndName(aceSystemDocs, "NetworkPolicy", controlPlaneDeploymentName+"-allow")
	if cpAllow == nil {
		t.Fatal("expected control-plane allow NetworkPolicy")
	}
	rendered, _ := yaml.Marshal(cpAllow)
	if !strings.Contains(string(rendered), "blob-storage") {
		t.Fatalf("control-plane NetworkPolicy must allow egress to blob-storage:\n%s", rendered)
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
		"--set", "config.builderImageDigest=buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if got, _ := data["APPLIANCE_BUILDER_IMAGE_DIGEST"].(string); got != "buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("APPLIANCE_BUILDER_IMAGE_DIGEST = %q, want rendered builder image", got)
	}
}

func TestArtifactProfilesRenderRealArtifactServerDependency(t *testing.T) {
	for _, profile := range []string{"storage", "storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			args := append(defaultRenderArgs(), "--set", "config.applianceProfile="+profile)
			if profile == "storage-landns" {
				args = append(args, "--set", "config.dnsReadyURL=http://dns-server.dns.svc.cluster.local:8181/ready")
			}
			docs := renderChart(t, args...)
			cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
			data, _ := at(cm, "data").(map[string]any)
			if got, _ := data["APPLIANCE_ARTIFACT_SERVER_BASE_URL"].(string); got != "http://appliance-registry.artifacts.svc.cluster.local:5000" {
				t.Fatalf("APPLIANCE_ARTIFACT_SERVER_BASE_URL = %q", got)
			}
			if got, _ := data["APPLIANCE_ARTIFACT_SERVER_ALLOW_FAKE"].(string); got != "false" {
				t.Fatalf("APPLIANCE_ARTIFACT_SERVER_ALLOW_FAKE = %q, want false", got)
			}
			policy := findByKindAndName(docs, "NetworkPolicy", controlPlaneDeploymentName+"-allow")
			rendered, _ := yaml.Marshal(policy)
			if !bytes.Contains(rendered, []byte("app.kubernetes.io/name: appliance-registry")) ||
				!bytes.Contains(rendered, []byte("kubernetes.io/metadata.name: artifacts")) ||
				!bytes.Contains(rendered, []byte("port: 5000")) {
				t.Fatalf("control-plane NetworkPolicy lacks registry-only egress:\n%s", rendered)
			}
		})
	}
}

func TestDNSProfilesRenderDNSReadyURL(t *testing.T) {
	for _, profile := range []string{"landns", "storage-landns", "builder-landns", "builder-storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			args := append(defaultRenderArgs(),
				"--set", "config.applianceProfile="+profile,
				"--set", "config.dnsReadyURL=http://dns-server.dns.svc.cluster.local:8181/ready",
			)
			switch profile {
			case "builder-landns", "builder-storage-landns":
				args = append(args,
					"--set", "config.buildCatalog.workProfiles[0].name=builder",
					"--set", "config.buildCatalog.workProfiles[0].repos[0].name=app",
					"--set", "config.buildCatalog.repos[0].name=app",
					"--set", "config.buildCatalog.repos[0].url=https://git.internal.example.com/team/app.git",
					"--set", "config.workspaceProvisionerImageDigest=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					"--set", "config.builderImageDigest=buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				)
			}
			docs := renderChart(t, args...)
			cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
			data, _ := at(cm, "data").(map[string]any)
			if got, _ := data["APPLIANCE_DNS_READY_URL"].(string); got != "http://dns-server.dns.svc.cluster.local:8181/ready" {
				t.Fatalf("APPLIANCE_DNS_READY_URL = %q", got)
			}
			if got, _ := data["APPLIANCE_DNS_BOOTSTRAP_HOSTNAME"].(string); got != "" {
				t.Fatalf("APPLIANCE_DNS_BOOTSTRAP_HOSTNAME = %q, want empty (API-owned records)", got)
			}
			if got, _ := data["APPLIANCE_DNS_ALLOW_FAKE_ZONE_SYNC"].(string); got != "false" {
				t.Fatalf("APPLIANCE_DNS_ALLOW_FAKE_ZONE_SYNC = %q, want false", got)
			}
			role := findByKindAndName(docs, "Role", controlPlaneDeploymentName+"-dns")
			if role == nil {
				t.Fatalf("expected Role %s-dns for zone ConfigMap patch", controlPlaneDeploymentName)
			}
			rendered, _ := yaml.Marshal(role)
			if !bytes.Contains(rendered, []byte("configmaps")) || !bytes.Contains(rendered, []byte("dns-server-config")) {
				t.Fatalf("dns Role missing ConfigMap patch rules:\n%s", rendered)
			}
			sa := findByKindAndName(docs, "ServiceAccount", controlPlaneDeploymentName)
			if got, _ := at(sa, "automountServiceAccountToken").(bool); !got {
				t.Fatalf("dns profiles must automount the service account token")
			}
			if profile == "builder-landns" || profile == "builder-storage-landns" {
				if findByKindAndName(docs, "Role", controlPlaneDeploymentName+"-workflows") == nil {
					t.Fatalf("expected workflow Role for build+dns profile %s", profile)
				}
			}
		})
	}
}

func TestValuesSchemaRejectsArtifactProfilesWithoutArtifactServerURL(t *testing.T) {
	requireHelm(t)
	for _, profile := range []string{"storage", "storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			valuesPath := filepath.Join(t.TempDir(), profile+"-without-artifact-server.yaml")
			values := fmt.Sprintf("config:\n  applianceProfile: %s\n  artifactServerBaseURL: \"\"\n", profile)
			if profile == "storage-landns" {
				values += "  dnsReadyURL: \"http://dns-server.dns.svc.cluster.local:8181/ready\"\n"
			}
			if err := os.WriteFile(valuesPath, []byte(values), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath).CombinedOutput()
			if err == nil {
				t.Fatalf("helm lint accepted %s without Artifact Server URL\n%s", profile, out)
			}
			if !bytes.Contains(out, []byte("artifactServerBaseURL")) {
				t.Fatalf("lint failed for wrong reason:\n%s", out)
			}
		})
	}
}

func TestValuesSchemaRejectsDNSProfilesWithoutDNSReadyURL(t *testing.T) {
	requireHelm(t)
	for _, profile := range []string{"landns", "storage-landns", "builder-landns", "builder-storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			valuesPath := filepath.Join(t.TempDir(), profile+"-without-dns.yaml")
			values := fmt.Sprintf("config:\n  applianceProfile: %s\n  dnsReadyURL: \"\"\n", profile)
			switch profile {
			case "storage-landns":
				values += "  artifactServerBaseURL: \"http://appliance-registry.artifacts.svc.cluster.local:5000\"\n  artifactServerAllowFake: false\n"
			case "builder-landns", "builder-storage-landns":
				values += `  artifactServerBaseURL: "http://appliance-registry.artifacts.svc.cluster.local:5000"
  artifactServerAllowFake: false
  workspaceProvisionerImageDigest: workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  builderImageDigest: buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  buildCatalog:
    workProfiles:
      - name: builder
        repos:
          - name: app
    repos:
      - name: app
        url: https://git.internal.example.com/team/app.git
`
			}
			if err := os.WriteFile(valuesPath, []byte(values), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath).CombinedOutput()
			if err == nil {
				t.Fatalf("helm lint accepted %s without dnsReadyURL\n%s", profile, out)
			}
			if !bytes.Contains(out, []byte("dnsReadyURL")) {
				t.Fatalf("lint failed for wrong reason:\n%s", out)
			}
		})
	}
}

func TestValuesSchemaRejectsUnsafeBuildCatalogPath(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "bad-values.yaml")
	values := []byte(`
config:
  applianceProfile: builder
  workspaceProvisionerImageDigest: workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  builderImageDigest: buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
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
        execution: script
        args: [build.sh]
        scriptPath: ../build.sh
        imageRepository: users/alice/app
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

func TestValuesSchemaAllowsBuilderWithEmptyBuildCatalog(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "builder-empty-catalog.yaml")
	values := []byte(`
config:
  applianceProfile: builder
  buildCatalog: {}
  workspaceProvisionerImageDigest: workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  builderImageDigest: buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  artifactServerBaseURL: http://appliance-registry.artifacts.svc.cluster.local:5000
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("writing test values: %v", err)
	}
	cmd := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm lint rejected builder with empty buildCatalog:\n%s", out)
	}
}

func TestValuesSchemaAllowsBuilderWithoutDay2BuilderImageDigest(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "builder-no-day2-builder.yaml")
	// builderImageDigest is an optional day-2 default (not packaged). Install
	// must succeed with empty/omitted; catalogs supply digests at build submit.
	values := []byte(`
config:
  applianceProfile: builder
  buildCatalog: {}
  workspaceProvisionerImageDigest: workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  builderImageDigest: ""
  artifactServerBaseURL: http://appliance-registry.artifacts.svc.cluster.local:5000
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("writing test values: %v", err)
	}
	cmd := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm lint rejected builder with empty day-2 builderImageDigest:\n%s", out)
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
  builderImageDigest: buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
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
		"--set", "config.builderImageDigest=buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	if volumeName, _ := at(pvc, "spec", "volumeName").(string); volumeName != "controlplane-workspaces" {
		t.Fatalf("workspace PVC volumeName = %q, want controlplane-workspaces", volumeName)
	}
	jobs := findByKind(docs, "Job")
	for _, job := range jobs {
		name, _ := at(job, "metadata", "name").(string)
		if strings.HasPrefix(name, "controlplane-workspace-storage-prep-") {
			t.Fatalf("workspace storage prep Job must be disabled by default (PSA restricted + helm --wait); got %q", name)
		}
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_WORKSPACE_ROOT_DIR"].(string); got != "/data/zon/workspaces" {
		t.Fatalf("APPLIANCE_WORKSPACE_ROOT_DIR = %q, want /data/zon/workspaces", got)
	}
	if got, _ := data["APPLIANCE_WORKSPACE_CLAIM_NAME"].(string); got != "controlplane-workspaces" {
		t.Fatalf("APPLIANCE_WORKSPACE_CLAIM_NAME = %q, want controlplane-workspaces", got)
	}
	if got, _ := data["APPLIANCE_WORKFLOW_INSTANCE_ID"].(string); got != "appliance" {
		t.Fatalf("APPLIANCE_WORKFLOW_INSTANCE_ID = %q, want appliance", got)
	}
	if got, _ := data["APPLIANCE_WORKFLOW_EXECUTOR_SERVICE_ACCOUNT"].(string); got != "appliance-workflows-executor" {
		t.Fatalf("APPLIANCE_WORKFLOW_EXECUTOR_SERVICE_ACCOUNT = %q, want appliance-workflows-executor", got)
	}
	if got, _ := data["APPLIANCE_WORKSPACE_PROVISIONER_IMAGE_DIGEST"].(string); got != "workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("APPLIANCE_WORKSPACE_PROVISIONER_IMAGE_DIGEST = %q, want workspace provisioner image", got)
	}
}

func TestBuilderWorkspacePrepareJobOptInFailsFast(t *testing.T) {
	requireHelm(t)
	extraArgs := append(defaultRenderArgs(),
		"--set", "config.applianceProfile=builder",
		"--set", "config.buildCatalog.workProfiles[0].name=builder",
		"--set", "config.buildCatalog.workProfiles[0].repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].url=https://git.internal.example.com/team/app.git",
		"--set", "config.workspaceProvisionerImageDigest=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"--set", "config.builderImageDigest=buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--set", "workspaceStorage.prepareJob.enabled=true",
	)
	args := append([]string{"template", "appliance", chartDir(t), "--namespace", "appliance"}, extraArgs...)
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err == nil {
		t.Fatalf("helm template unexpectedly accepted workspaceStorage.prepareJob.enabled=true\n%s", out)
	}
	if !bytes.Contains(out, []byte("workspaceStorage.prepareJob.enabled is not supported")) {
		t.Fatalf("helm template failed for the wrong reason:\n%s", out)
	}
}

func TestBuilderWorkflowRBACRenders(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "config.applianceProfile=builder",
		"--set", "config.buildCatalog.workProfiles[0].name=builder",
		"--set", "config.buildCatalog.workProfiles[0].repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].name=app",
		"--set", "config.buildCatalog.repos[0].url=https://git.internal.example.com/team/app.git",
		"--set", "config.workspaceProvisionerImageDigest=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"--set", "config.builderImageDigest=buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)...)
	dep := findByKindAndName(docs, "Deployment", controlPlaneDeploymentName)
	if dep == nil {
		t.Fatal("expected control-plane Deployment")
	}
	if automount, _ := at(dep, "spec", "template", "spec", "automountServiceAccountToken").(bool); !automount {
		t.Fatal("builder/workflows deployment should mount a service account token")
	}
	role := findByKindAndName(docs, "Role", controlPlaneDeploymentName+"-workflows")
	if role == nil {
		t.Fatal("expected workflow Role for builder/workflows")
	}
	if ns, _ := at(role, "metadata", "namespace").(string); ns != "appliance-builds" {
		t.Fatalf("workflow Role namespace = %q, want appliance-builds", ns)
	}
	rules, _ := at(role, "rules").([]any)
	if !roleRuleAllowsResource(rules, "secrets", "create", "get", "list", "update", "delete") {
		t.Fatal("workflow Role should allow create/get/list/update/delete on secrets for named builder Git access")
	}
	if rb := findByKindAndName(docs, "RoleBinding", controlPlaneDeploymentName+"-workflows"); rb == nil {
		t.Fatal("expected workflow RoleBinding for builder/workflows")
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if _, ok := data["APPLIANCE_WORKFLOW_NAMESPACE"]; ok {
		t.Fatal("control-plane ConfigMap should not expose APPLIANCE_WORKFLOW_NAMESPACE once the namespace is fixed in code")
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

func TestAppsNamespacePlacesUIAndHostAwayFromControlplane(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "namespace.name=ace-system",
		"--set", "appsNamespace.name=ace-apps",
		"--set", "hostAgent.enabled=true",
		"--set", "hostAgent.image.reference=registry.local/host-agent@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)...)

	assertNS := func(kind, name, wantNS string) {
		t.Helper()
		doc := findByKindAndName(docs, kind, name)
		if doc == nil {
			t.Fatalf("expected %s/%s", kind, name)
		}
		if got, _ := at(doc, "metadata", "namespace").(string); got != wantNS {
			t.Fatalf("%s/%s namespace = %q, want %q", kind, name, got, wantNS)
		}
	}
	assertNS("Deployment", controlPlaneDeploymentName, "ace-system")
	assertNS("Service", controlPlaneServiceName, "ace-system")
	assertNS("Deployment", controlPlaneUIName, "ace-apps")
	assertNS("Service", controlPlaneUIName, "ace-apps")
	assertNS("Deployment", "host-agent", "ace-apps")
	assertNS("Deployment", automationRuntimeName, "ace-apps")

	// IngressRoute in controlplane NS must name the UI Service in ace-apps.
	route := findByKindAndName(docs, "IngressRoute", controlPlaneDeploymentName)
	if route == nil {
		t.Fatal("expected IngressRoute")
	}
	routes, _ := at(route, "spec", "routes").([]any)
	var foundUICrossNS bool
	for _, raw := range routes {
		r, _ := raw.(map[string]any)
		match, _ := r["match"].(string)
		if !strings.Contains(match, "PathPrefix(`/`)") {
			continue
		}
		svcs, _ := r["services"].([]any)
		for _, sraw := range svcs {
			s, _ := sraw.(map[string]any)
			name, _ := s["name"].(string)
			ns, _ := s["namespace"].(string)
			if name == controlPlaneUIName && ns == "ace-apps" {
				foundUICrossNS = true
			}
		}
	}
	if !foundUICrossNS {
		t.Fatal("expected UI IngressRoute service to reference ui-server in ace-apps")
	}

	// Only the real ClusterIP Service in apps (no ExternalName alias).
	var uiServices []map[string]any
	for _, d := range findByKind(docs, "Service") {
		if n, _ := at(d, "metadata", "name").(string); n == controlPlaneUIName {
			uiServices = append(uiServices, d)
		}
	}
	if len(uiServices) != 1 {
		t.Fatalf("expected 1 ui-server Service, got %d", len(uiServices))
	}
	if ns, _ := at(uiServices[0], "metadata", "namespace").(string); ns != "ace-apps" {
		t.Fatalf("ui-server Service namespace = %q", ns)
	}

	cm := findByKindAndName(docs, "ConfigMap", controlPlaneUIConfigName)
	if cm == nil {
		t.Fatal("ui configmap missing")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_CONTROL_PLANE_BASE_URL"].(string); got != "http://controlplane.ace-system.svc.cluster.local:8080" {
		t.Fatalf("UI control plane URL = %q", got)
	}
	cpCM := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	cpData, _ := at(cpCM, "data").(map[string]any)
	if got, _ := cpData["APPLIANCE_AUTOMATION_RUNTIME_BASE_URL"].(string); got != "http://automation-runtime.ace-apps.svc.cluster.local:8082" {
		t.Fatalf("automation runtime URL = %q", got)
	}
}

func TestImagePullSecretsRenderedOnControlplaneAndAppsDeployments(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "namespace.name=ace-system",
		"--set", "appsNamespace.name=ace-apps",
		"--set", "imagePullSecrets[0].name=appliance-image-pull",
		"--set", "hostAgent.enabled=true",
		"--set", "hostAgent.image.reference=registry.local/host-agent@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)...)

	for _, name := range []string{controlPlaneDeploymentName, controlPlaneUIName, "host-agent", automationRuntimeName} {
		doc := findByKindAndName(docs, "Deployment", name)
		if doc == nil {
			t.Fatalf("expected Deployment/%s", name)
		}
		ips, _ := at(doc, "spec", "template", "spec", "imagePullSecrets").([]any)
		if len(ips) != 1 {
			t.Fatalf("%s imagePullSecrets = %#v, want one entry", name, ips)
		}
		entry, _ := ips[0].(map[string]any)
		if got, _ := entry["name"].(string); got != "appliance-image-pull" {
			t.Fatalf("%s imagePullSecrets[0].name = %q", name, got)
		}
	}
}

func TestApplicationNamespaceIsAlwaysProvisioned(t *testing.T) {
	docs := renderChart(t, "--set", "namespace.name=ace-system", "--set", "appsNamespace.name=ace-apps", "--set", "config.applicationManagementEnabled=true")
	ns := findByKindAndName(docs, "Namespace", "apps")
	if ns != nil {
		t.Fatal("application namespace must be installer-owned, not Helm-owned")
	}
	marker := findByKindAndName(docs, "ConfigMap", "appliance-application-management")
	if marker == nil {
		t.Fatal("expected application-management namespace marker")
	}
	if got, _ := at(marker, "metadata", "namespace").(string); got != "apps" {
		t.Fatalf("application-management marker namespace = %q", got)
	}
}

func TestWorkflowsBuildNamespaceIsInstallerOwned(t *testing.T) {
	docs := renderChart(t,
		"--set", "config.applianceProfile=builder",
		"--set", "namespace.name=ace-system",
		"--set", "appsNamespace.name=ace-apps",
		"--set", "config.workspaceProvisionerImageDigest=workspace-provisioner@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"--set", "config.builderImageDigest=buildah@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	)
	ns := findByKindAndName(docs, "Namespace", "appliance-builds")
	if ns != nil {
		t.Fatal("workflows build namespace must be installer-owned, not Helm-owned")
	}
}

func containsString(values []any, want string) bool {
	for _, value := range values {
		if got, _ := value.(string); got == want {
			return true
		}
	}
	return false
}
