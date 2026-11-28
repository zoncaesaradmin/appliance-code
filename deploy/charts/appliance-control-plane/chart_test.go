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
	actionRuntimeName          = "action-runtime"
	actionRuntimeConfig        = "action-runtime-config"
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

func TestPostgresProviderUsesOnlyASecretBackedDSN(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "database.driver=postgres",
		"--set", "database.postgres.connectionSecretName=managed-postgres-connection",
		"--set", "database.postgres.connectionSecretKey=dsn",
	)...)
	dep := findByKindAndName(docs, "Deployment", controlPlaneDeploymentName)
	if dep == nil {
		t.Fatal("expected control-plane Deployment")
	}
	containers, _ := at(dep, "spec", "template", "spec", "containers").([]any)
	if len(containers) != 1 {
		t.Fatalf("expected one control-plane container, got %d", len(containers))
	}
	container, _ := containers[0].(map[string]any)
	env, _ := container["env"].([]any)
	for _, raw := range env {
		item, _ := raw.(map[string]any)
		if item["name"] != "APPLIANCE_DATABASE_DSN" {
			continue
		}
		secretRef, _ := at(item, "valueFrom", "secretKeyRef").(map[string]any)
		if secretRef["name"] != "managed-postgres-connection" || secretRef["key"] != "dsn" {
			t.Fatalf("PostgreSQL DSN secret reference = %#v, want managed-postgres-connection/dsn", secretRef)
		}
		return
	}
	t.Fatal("PostgreSQL DSN must be injected through a Secret reference")
}

func TestLocalPostgresProfileRendersPinnedStatefulSet(t *testing.T) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "rollout.aceInfra.enabled=true",
		"--set", "database.driver=postgres",
		"--set", "database.postgres.connectionSecretName=controlplane-database-connection",
		"--set", "database.postgres.local.enabled=true",
		"--set", "database.postgres.local.image.digest="+digest,
	)...)
	statefulSet := findByKindAndName(docs, "StatefulSet", "platform-postgres")
	if statefulSet == nil {
		t.Fatal("local PostgreSQL profile must render its StatefulSet")
	}
	metadata, _ := statefulSet["metadata"].(map[string]any)
	if namespace, _ := metadata["namespace"].(string); namespace != "ace-infra" {
		t.Fatalf("PostgreSQL StatefulSet namespace = %q, want ace-infra", namespace)
	}
	containers, _ := at(statefulSet, "spec", "template", "spec", "containers").([]any)
	if len(containers) != 1 {
		t.Fatalf("PostgreSQL containers = %d, want 1", len(containers))
	}
	container, _ := containers[0].(map[string]any)
	if image, _ := container["image"].(string); image != "registry.local/postgres@"+digest {
		t.Fatalf("PostgreSQL image = %q, want digest-pinned image", image)
	}
}

func TestLocalPostgresAllowsApplicationIdentity(t *testing.T) {
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "appsNamespace.name=ace-apps",
		"--set", "config.enabledCapabilities={application-identity}",
		"--set", "applicationServices.identity.enabled=true",
		"--set", "rollout.aceInfra.enabled=true",
		"--set", "database.driver=postgres",
		"--set", "database.postgres.connectionSecretName=controlplane-database-connection",
		"--set", "database.postgres.local.enabled=true",
		"--set", "database.postgres.local.image.digest="+digest,
	)...)
	policy := findByKindAndName(docs, "NetworkPolicy", "platform-postgres-allow")
	if policy == nil {
		t.Fatal("expected local PostgreSQL NetworkPolicy")
	}
	rendered, _ := yaml.Marshal(policy)
	if !bytes.Contains(rendered, []byte("kubernetes.io/metadata.name: ace-apps")) ||
		!bytes.Contains(rendered, []byte("app.kubernetes.io/name: identity")) {
		t.Fatalf("PostgreSQL NetworkPolicy must allow application identity egress:\n%s", rendered)
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
		t.Error("automountServiceAccountToken should be false when no privileged capability is enabled")
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

func TestApplianceCAVolumeMountsPublicCertOnly(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	dep := findByKindAndName(docs, "Deployment", controlPlaneDeploymentName)
	if dep == nil {
		t.Fatal("expected control-plane Deployment")
	}
	volumes, _ := at(dep, "spec", "template", "spec", "volumes").([]any)
	foundVolume := false
	for _, raw := range volumes {
		volume, _ := raw.(map[string]any)
		if name, _ := volume["name"].(string); name != "appliance-ca" {
			continue
		}
		foundVolume = true
		mode, _ := at(volume, "secret", "defaultMode").(int)
		if mode != 0o440 {
			t.Fatalf("appliance-ca secret defaultMode = %#o, want 0440", mode)
		}
		secretName, _ := at(volume, "secret", "secretName").(string)
		if secretName != "appliance-ca" {
			t.Fatalf("appliance-ca secretName = %q, want appliance-ca", secretName)
		}
		items, _ := at(volume, "secret", "items").([]any)
		if len(items) != 1 {
			t.Fatalf("appliance-ca items = %#v, want only ca.crt", items)
		}
		item, _ := items[0].(map[string]any)
		key, _ := at(item, "key").(string)
		path, _ := at(item, "path").(string)
		if key != "ca.crt" || path != "ca.crt" {
			t.Fatalf("appliance-ca item = key=%q path=%q, want ca.crt", key, path)
		}
	}
	if !foundVolume {
		t.Fatal("expected appliance-ca secret volume on control-plane Deployment")
	}
	containers, _ := at(dep, "spec", "template", "spec", "containers").([]any)
	if len(containers) == 0 {
		t.Fatal("expected control-plane container")
	}
	container, _ := containers[0].(map[string]any)
	mounts, _ := at(container, "volumeMounts").([]any)
	foundMount := false
	for _, raw := range mounts {
		mount, _ := raw.(map[string]any)
		if name, _ := mount["name"].(string); name != "appliance-ca" {
			continue
		}
		foundMount = true
		if mountPath, _ := mount["mountPath"].(string); mountPath != "/var/run/appliance/tls" {
			t.Fatalf("appliance-ca mountPath = %q, want /var/run/appliance/tls", mountPath)
		}
		if ro, _ := mount["readOnly"].(bool); !ro {
			t.Fatal("appliance-ca volumeMount must be readOnly")
		}
	}
	if !foundMount {
		t.Fatal("expected appliance-ca volumeMount on control-plane container")
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneDeploymentName+"-config")
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	if path, _ := at(cm, "data", "APPLIANCE_TLS_CA_CERT_PATH").(string); path != "/var/run/appliance/tls/ca.crt" {
		t.Fatalf("APPLIANCE_TLS_CA_CERT_PATH = %q, want /var/run/appliance/tls/ca.crt", path)
	}
}

func TestRenderedChartContainsNoExternalHelperImages(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
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

func TestActionRuntimeResourcesRenderByDefault(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	if findByKindAndName(docs, "Deployment", actionRuntimeName) == nil {
		t.Fatal("expected action runtime Deployment")
	}
	if findByKindAndName(docs, "Service", actionRuntimeName) == nil {
		t.Fatal("expected action runtime Service")
	}
	if findByKindAndName(docs, "ConfigMap", actionRuntimeConfig) == nil {
		t.Fatal("expected action runtime ConfigMap")
	}
	if findByKindAndName(docs, "NetworkPolicy", actionRuntimeName+"-allow") == nil {
		t.Fatal("expected action runtime NetworkPolicy")
	}
	if findByKindAndName(docs, "PersistentVolumeClaim", actionRuntimeName+"-data") == nil {
		t.Fatal("expected action runtime PVC")
	}
	dep := findByKindAndName(docs, "Deployment", actionRuntimeName)
	sec, _ := at(dep, "spec", "template", "spec", "securityContext").(map[string]any)
	if runAsUser, _ := sec["runAsUser"].(int); runAsUser != 10007 {
		t.Fatalf("action runtime runAsUser = %d, want 10007 (must not reuse inference 10006)", runAsUser)
	}
	volumes, _ := at(dep, "spec", "template", "spec", "volumes").([]any)
	var sawOwnPVC bool
	for _, raw := range volumes {
		volume, _ := raw.(map[string]any)
		if name, _ := volume["name"].(string); name != "data" {
			continue
		}
		claim, _ := at(volume, "persistentVolumeClaim", "claimName").(string)
		if claim == actionRuntimeName+"-data" {
			sawOwnPVC = true
		}
		if claim == controlPlaneDeploymentName+"-data" {
			t.Fatal("action runtime must not mount the control-plane data PVC (single-writer SQLite)")
		}
	}
	if !sawOwnPVC {
		t.Fatal("expected action runtime to mount its own data PVC")
	}
}

func TestControlPlaneConfigPointsToActionRuntimeService(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_ACTION_RUNTIME_BASE_URL"].(string); got != "http://action-runtime.appliance.svc.cluster.local:8082" {
		t.Fatalf("APPLIANCE_ACTION_RUNTIME_BASE_URL = %q, want http://action-runtime.appliance.svc.cluster.local:8082", got)
	}
}

func TestActionRuntimeCanBeDisabledByCapabilityDerivedValues(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(), "--set", "actionRuntime.enabled=false")...)
	for _, resource := range []struct {
		kind string
		name string
	}{
		{kind: "Deployment", name: actionRuntimeName},
		{kind: "Service", name: actionRuntimeName},
		{kind: "ConfigMap", name: actionRuntimeConfig},
		{kind: "NetworkPolicy", name: actionRuntimeName + "-allow"},
		{kind: "PersistentVolumeClaim", name: actionRuntimeName + "-data"},
	} {
		if findByKindAndName(docs, resource.kind, resource.name) != nil {
			t.Fatalf("actions-disabled render contains %s/%s", resource.kind, resource.name)
		}
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if cm == nil {
		t.Fatal("expected control-plane ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_ACTION_RUNTIME_BASE_URL"].(string); got != "" {
		t.Fatalf("APPLIANCE_ACTION_RUNTIME_BASE_URL = %q, want empty when actions are disabled", got)
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
		case match == "(PathPrefix(`/api/v1`) || PathPrefix(`/mcp`) || PathPrefix(`/ai/v1`) || PathPrefix(`/inference/v1`) || PathPrefix(`/video/v1`))" && name == controlPlaneServiceName:
			if priority != 100 {
				t.Errorf("API route priority = %v, want 100", route["priority"])
			}
			apiRouteOK = true
		case match == "PathPrefix(`/`) && !PathPrefix(`/api`) && !PathPrefix(`/mcp`) && !PathPrefix(`/ai`) && !PathPrefix(`/inference`) && !PathPrefix(`/video`) && !PathPrefix(`/webui`) && !PathPrefix(`/v2`)" && name == controlPlaneUIName:
			if priority != 1 {
				t.Errorf("UI route priority = %v, want 1", route["priority"])
			}
			uiRouteOK = true
		}
	}
	if !apiRouteOK {
		t.Error("expected /api/v1, /mcp, /ai/v1, /inference/v1, and /video/v1 route to target control-plane service")
	}
	if !uiRouteOK {
		t.Error("expected / route to target UI service with API/MCP/AI/inference/video/registry exclusions")
	}
}

func TestIngressRouteExposesWebUIOnlyWhenInstallerEnablesPair(t *testing.T) {
	base := renderChart(t, defaultRenderArgs()...)
	routes := findByKind(base, "IngressRoute")
	if len(routes) != 1 {
		t.Fatalf("expected one IngressRoute, got %d", len(routes))
	}
	for _, raw := range at(routes[0], "spec", "routes").([]any) {
		if raw.(map[string]any)["match"].(string) == "PathPrefix(`/webui`)" {
			t.Fatal("WebUI route must be absent unless the paired delivery artifacts enable it")
		}
	}

	docs := renderChart(t, append(defaultRenderArgs(), "--set", "ingress.webUIEnabled=true")...)
	routes = findByKind(docs, "IngressRoute")
	found := false
	for _, raw := range at(routes[0], "spec", "routes").([]any) {
		route := raw.(map[string]any)
		if route["match"] != "PathPrefix(`/webui`)" {
			continue
		}
		services := route["services"].([]any)
		service := services[0].(map[string]any)
		if service["name"] != "inference-gateway-open-webui-gateway" || service["namespace"] != "inference" || route["priority"] != 110 {
			t.Fatalf("unexpected WebUI bridge route: %#v", route)
		}
		found = true
	}
	if !found {
		t.Fatal("expected HTTPS WebUI bridge route when installer enables the image pair")
	}
}

func TestIngressRouteOmitsPlaintextHTTPWithoutCapability(t *testing.T) {
	docs := renderChart(t, defaultRenderArgs()...)
	routes := findByKind(docs, "IngressRoute")
	if len(routes) != 1 {
		t.Fatalf("expected exactly one IngressRoute, got %d", len(routes))
	}
	tlsRoute := findByKindAndName(docs, "IngressRoute", controlPlaneDeploymentName)
	if tlsRoute == nil {
		t.Fatal("expected TLS IngressRoute")
	}
	entryPoints, _ := at(tlsRoute, "spec", "entryPoints").([]any)
	if fmt.Sprint(entryPoints) != "[websecure]" {
		t.Fatalf("TLS IngressRoute entryPoints = %#v, want [websecure]", entryPoints)
	}
	if at(tlsRoute, "spec", "tls", "secretName") == nil {
		t.Fatal("TLS IngressRoute must declare tls.secretName")
	}
	if findByKindAndName(docs, "IngressRoute", controlPlaneDeploymentName+"-http") != nil {
		t.Fatal("plaintext HTTP IngressRoute must be omitted when plaintext-http is not enabled")
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneUIConfigName)
	if cm == nil {
		t.Fatal("expected UI ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_UI_COOKIE_SECURE"].(string); got != "true" {
		t.Fatalf("APPLIANCE_UI_COOKIE_SECURE = %q, want true", got)
	}
}

func TestIngressRouteRendersPlaintextHTTPWhenCapabilityEnabled(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(), "--set", "config.enabledCapabilities[0]=plaintext-http")...)
	tlsRoute := findByKindAndName(docs, "IngressRoute", controlPlaneDeploymentName)
	if tlsRoute == nil {
		t.Fatal("expected TLS IngressRoute")
	}
	httpRoute := findByKindAndName(docs, "IngressRoute", controlPlaneDeploymentName+"-http")
	if httpRoute == nil {
		t.Fatal("expected plaintext HTTP IngressRoute when plaintext-http is enabled")
	}
	entryPoints, _ := at(httpRoute, "spec", "entryPoints").([]any)
	if fmt.Sprint(entryPoints) != "[web]" {
		t.Fatalf("plaintext HTTP IngressRoute entryPoints = %#v, want [web]", entryPoints)
	}
	if at(httpRoute, "spec", "tls") != nil {
		t.Fatal("plaintext HTTP IngressRoute must not declare tls")
	}
	tlsEntryPoints, _ := at(tlsRoute, "spec", "entryPoints").([]any)
	if fmt.Sprint(tlsEntryPoints) != "[websecure]" {
		t.Fatalf("TLS IngressRoute entryPoints = %#v, want [websecure]", tlsEntryPoints)
	}
	if at(tlsRoute, "spec", "tls", "secretName") == nil {
		t.Fatal("TLS IngressRoute must keep tls.secretName")
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneUIConfigName)
	if cm == nil {
		t.Fatal("expected UI ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_UI_COOKIE_SECURE"].(string); got != "false" {
		t.Fatalf("APPLIANCE_UI_COOKIE_SECURE = %q, want false when plaintext-http is enabled", got)
	}
}

func TestIngressRouteRendersCloudflareHTTPOriginWithoutDisablingSecureCookies(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(), "--set", "ingress.tlsTermination=cloudflare")...)
	tlsRoute := findByKindAndName(docs, "IngressRoute", controlPlaneDeploymentName)
	if tlsRoute == nil {
		t.Fatal("expected TLS IngressRoute")
	}
	httpRoute := findByKindAndName(docs, "IngressRoute", controlPlaneDeploymentName+"-http")
	if httpRoute == nil {
		t.Fatal("expected HTTP origin IngressRoute when tlsTermination=cloudflare")
	}
	entryPoints, _ := at(httpRoute, "spec", "entryPoints").([]any)
	if fmt.Sprint(entryPoints) != "[web]" {
		t.Fatalf("Cloudflare HTTP origin IngressRoute entryPoints = %#v, want [web]", entryPoints)
	}
	if at(httpRoute, "spec", "tls") != nil {
		t.Fatal("Cloudflare HTTP origin IngressRoute must not declare tls")
	}
	tlsEntryPoints, _ := at(tlsRoute, "spec", "entryPoints").([]any)
	if fmt.Sprint(tlsEntryPoints) != "[websecure]" {
		t.Fatalf("TLS IngressRoute entryPoints = %#v, want [websecure]", tlsEntryPoints)
	}
	if at(tlsRoute, "spec", "tls", "secretName") == nil {
		t.Fatal("TLS IngressRoute must keep tls.secretName")
	}
	cm := findByKindAndName(docs, "ConfigMap", controlPlaneUIConfigName)
	if cm == nil {
		t.Fatal("expected UI ConfigMap")
	}
	data, _ := at(cm, "data").(map[string]any)
	if got, _ := data["APPLIANCE_UI_COOKIE_SECURE"].(string); got != "true" {
		t.Fatalf("APPLIANCE_UI_COOKIE_SECURE = %q, want true for Cloudflare TLS termination", got)
	}
}

func TestChannelIntegrationIngressSupportsCloudflareHTTPOrigin(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "config.enabledCapabilities[0]=base",
		"--set", "config.enabledCapabilities[1]=channel-integrations",
		"--set", "channelIntegration.enabled=true",
		"--set", "channelIntegration.ingress.entryPoints[0]=web",
		"--set", "channelIntegration.ingress.host=local-channels.zonited.com",
		"--set", "channelIntegration.ingress.tls.enabled=false",
	)...)
	route := findByKindAndName(docs, "IngressRoute", "channel-integration")
	if route == nil {
		t.Fatal("expected channel integration IngressRoute")
	}
	entryPoints, _ := at(route, "spec", "entryPoints").([]any)
	if fmt.Sprint(entryPoints) != "[web]" {
		t.Fatalf("channel integration entryPoints = %#v, want [web]", entryPoints)
	}
	routes, _ := at(route, "spec", "routes").([]any)
	if len(routes) != 1 {
		t.Fatalf("channel integration route count = %d, want 1", len(routes))
	}
	firstRoute, _ := routes[0].(map[string]any)
	if got, _ := firstRoute["match"].(string); got != "Host(`local-channels.zonited.com`) && PathPrefix(`/webhook`)" {
		t.Fatalf("channel integration route match = %q", got)
	}
	if at(route, "spec", "tls") != nil {
		t.Fatal("Cloudflare channel integration route must not declare TLS")
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
	docs := renderChart(t, "--set", "namespace.create=false", "--set", "persistence.enabled=false", "--set", "actionRuntime.persistence.enabled=false", "--set", "ingress.enabled=false", "--set", "ui.enabled=false")
	if findByKindAndName(docs, "Namespace", "ace-system") != nil {
		t.Error("namespace.create=false should omit the control-plane Namespace object")
	}
	if findByKindAndName(docs, "Namespace", "apps") != nil {
		t.Error("application namespace must be provisioned by the installer, not rendered by Helm")
	}
	// Control-plane and action-runtime PVCs honor persistence.enabled.
	// Foundation blob-storage always renders its own claim (hostPath-backed).
	if findByKindAndName(docs, "PersistentVolumeClaim", controlPlaneDeploymentName+"-data") != nil {
		t.Error("persistence.enabled=false should omit the control-plane PersistentVolumeClaim")
	}
	if findByKindAndName(docs, "PersistentVolumeClaim", actionRuntimeName+"-data") != nil {
		t.Error("actionRuntime.persistence.enabled=false should omit the action-runtime PersistentVolumeClaim")
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

func TestBlobStorageBelongsToAceInfraRolloutOnly(t *testing.T) {
	// zonctl installs the same chart as multiple Helm releases. Blob-storage
	// must render only for the ace-infra release before the control plane.
	aceAppsDocs := renderChart(t, append(defaultRenderArgs(),
		"--set", "rollout.aceInfra.enabled=false",
		"--set", "rollout.aceSystem.enabled=false",
		"--set", "rollout.aceApps.enabled=true",
		"--set", "rollout.dnsSupport.enabled=false",
	)...)
	if findByKindAndName(aceAppsDocs, "Deployment", "blob-storage") != nil {
		t.Fatal("ace-apps rollout must not render blob-storage Deployment")
	}
	if findByKindAndName(aceAppsDocs, "Service", "blob-storage") != nil {
		t.Fatal("ace-apps rollout must not render blob-storage Service")
	}

	aceSystemDocs := renderChart(t, append(defaultRenderArgs(),
		"--set", "rollout.aceInfra.enabled=false",
		"--set", "rollout.aceSystem.enabled=true",
		"--set", "rollout.aceApps.enabled=false",
		"--set", "rollout.dnsSupport.enabled=false",
	)...)
	if findByKindAndName(aceSystemDocs, "Deployment", "blob-storage") != nil {
		t.Fatal("ace-system rollout must not render blob-storage Deployment")
	}
	infraDocs := renderChart(t, append(defaultRenderArgs(),
		"--set", "rollout.aceInfra.enabled=true",
		"--set", "rollout.aceSystem.enabled=false",
		"--set", "rollout.aceApps.enabled=false",
		"--set", "rollout.dnsSupport.enabled=false",
	)...)
	blobDeploy := findByKindAndName(infraDocs, "Deployment", "blob-storage")
	if blobDeploy == nil {
		t.Fatal("ace-infra rollout must render blob-storage Deployment")
	}
	blobNS, _ := at(blobDeploy, "metadata", "namespace").(string)
	if blobNS != "ace-infra" {
		t.Fatalf("blob-storage namespace = %q, want ace-infra", blobNS)
	}
	if initContainers, _ := at(blobDeploy, "spec", "template", "spec", "initContainers").([]any); len(initContainers) != 0 {
		t.Fatalf("blob-storage must not use a root init container under restricted PSA, got %v", initContainers)
	}
	blobSvc := findByKindAndName(infraDocs, "Service", "blob-storage")
	if blobSvc == nil {
		t.Fatal("ace-infra rollout must render blob-storage Service")
	}
	if ns, _ := at(blobSvc, "metadata", "namespace").(string); ns != blobNS {
		t.Fatalf("blob-storage Service namespace = %q, want %q", ns, blobNS)
	}
	if findByKindAndName(infraDocs, "NetworkPolicy", "blob-storage-allow") == nil {
		t.Fatal("expected pod-scoped blob-storage-allow NetworkPolicy")
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

func TestGeoTokenIsPreparedForAceSystemBeforeGeoWorkload(t *testing.T) {
	args := append(defaultRenderArgs(),
		"--set", "geo.enabled=true",
		"--set", "geo.postgres.image.digest=sha256:"+strings.Repeat("a", 64),
		"--set", "rollout.aceInfra.enabled=false",
		"--set", "rollout.aceSystem.enabled=true",
		"--set", "rollout.aceApps.enabled=false",
		"--set", "rollout.dnsSupport.enabled=false",
	)
	docs := renderChart(t, args...)
	secret := findByKindAndName(docs, "Secret", "geo-internal-auth")
	if secret == nil {
		t.Fatal("ace-system rollout must create the Geo token Secret")
	}
	if namespace, _ := at(secret, "metadata", "namespace").(string); namespace != "appliance" {
		t.Fatalf("Geo token Secret namespace = %q, want appliance", namespace)
	}
	dep := findByKindAndName(docs, "Deployment", controlPlaneDeploymentName)
	if dep == nil {
		t.Fatal("expected control-plane Deployment")
	}
	if !strings.Contains(fmt.Sprint(at(dep, "spec", "template", "spec", "containers")), "geo-internal-token") {
		t.Fatal("ace-system control-plane must mount the Geo token")
	}
	config := findByKindAndName(docs, "ConfigMap", controlPlaneConfigMapName)
	if config == nil || at(config, "data", "APPLIANCE_GEO_INTERNAL_TOKEN_FILE") != "/var/run/appliance/geo/token" {
		t.Fatal("ace-system control-plane must receive the Geo token file path")
	}

	appsDocs := renderChart(t, append(defaultRenderArgs(),
		"--set", "geo.enabled=true",
		"--set", "geo.postgres.image.digest=sha256:"+strings.Repeat("a", 64),
		"--set", "rollout.aceInfra.enabled=false",
		"--set", "rollout.aceSystem.enabled=false",
		"--set", "rollout.aceApps.enabled=true",
		"--set", "rollout.dnsSupport.enabled=false",
	)...)
	geoNamespace := findByKindAndName(appsDocs, "Namespace", "geo")
	if geoNamespace == nil || at(geoNamespace, "metadata", "labels", "pod-security.kubernetes.io/enforce") != "privileged" {
		t.Fatal("Geo namespace must declare the privileged PSA exception for its host-visible log mount")
	}
	geoDeployment := findByKindAndName(appsDocs, "Deployment", "geo-data-api")
	if geoDeployment == nil || !strings.Contains(fmt.Sprint(at(geoDeployment, "spec", "template", "spec", "volumes")), "/data/zon/logs/geo-data-api") {
		t.Fatal("Geo data API must retain its host-visible log mount")
	}
	geoPostgres := findByKindAndName(appsDocs, "StatefulSet", "geo-postgres")
	if geoPostgres == nil || !strings.Contains(fmt.Sprint(at(geoPostgres, "spec", "template", "spec", "containers")), "ALTER ROLE geo PASSWORD") {
		t.Fatal("Geo PostgreSQL must reconcile the persisted role password on startup")
	}
}

func TestApplicationCoreRolloutOwnsDedicatedNamespace(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "rollout.aceInfra.enabled=false",
		"--set", "rollout.aceSystem.enabled=false",
		"--set", "rollout.aceAppCore.enabled=true",
		"--set", "rollout.aceApps.enabled=false",
		"--set", "rollout.dnsSupport.enabled=false",
		"--set", "appCoreNamespace.name=ace-app-core",
	)...)
	ns := findByKindAndName(docs, "Namespace", "ace-app-core")
	if ns == nil {
		t.Fatal("ace-app-core rollout must render the dedicated namespace")
	}
	marker := findByKindAndName(docs, "ConfigMap", "appliance-application-core")
	if marker == nil {
		t.Fatal("ace-app-core rollout must render its ownership marker")
	}
	if got, _ := at(marker, "metadata", "namespace").(string); got != "ace-app-core" {
		t.Fatalf("application-core marker namespace = %q, want ace-app-core", got)
	}
	if findByKindAndName(docs, "Deployment", controlPlaneDeploymentName) != nil {
		t.Fatal("ace-app-core rollout must not render the control-plane Deployment")
	}
	if findByKindAndName(docs, "Deployment", "blob-storage") != nil {
		t.Fatal("ace-app-core rollout must not render infrastructure workloads")
	}
}

func TestApplicationIdentityEndpointBelongsToAceAppsRollout(t *testing.T) {
	base := []string{
		"--set", "applicationServices.identity.enabled=true",
		"--set", "config.enabledCapabilities={application-identity,application-auth}",
		"--set", "appsNamespace.name=ace-apps",
		"--set", "appCoreNamespace.name=ace-app-core",
		"--set", "rollout.aceInfra.enabled=false",
		"--set", "rollout.aceSystem.enabled=false",
		"--set", "rollout.aceApps.enabled=false",
		"--set", "rollout.aceAppCore.enabled=true",
		"--set", "rollout.dnsSupport.enabled=false",
	}
	appCoreDocs := renderChart(t, append(defaultRenderArgs(), base...)...)
	if findByKindAndName(appCoreDocs, "ConfigMap", "application-auth-endpoint") != nil {
		t.Fatal("application-auth-endpoint must not render in the ace-app-core rollout")
	}
	if findByKindAndName(appCoreDocs, "NetworkPolicy", "identity-allow") != nil {
		t.Fatal("identity NetworkPolicy must not render in the ace-app-core rollout")
	}
	appsArgs := append([]string{}, base...)
	appsArgs[13] = "rollout.aceApps.enabled=true"
	appsArgs[15] = "rollout.aceAppCore.enabled=false"
	appsDocs := renderChart(t, append(defaultRenderArgs(), appsArgs...)...)
	endpoint := findByKindAndName(appsDocs, "ConfigMap", "application-auth-endpoint")
	if endpoint == nil {
		t.Fatal("application-auth-endpoint must render in the ace-apps rollout")
	}
	if got, _ := at(endpoint, "metadata", "namespace").(string); got != "ace-apps" {
		t.Fatalf("application-auth-endpoint namespace = %q, want ace-apps", got)
	}
	if findByKindAndName(appsDocs, "NetworkPolicy", "identity-allow") == nil {
		t.Fatal("identity NetworkPolicy must render in the ace-apps rollout")
	}
}

func TestArtifactProfilesRenderRealArtifactServerDependency(t *testing.T) {
	for _, profile := range []string{"storage", "storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			args := append(defaultRenderArgs(), "--set", "config.applianceProfile="+profile, "--set", "config.enabledCapabilities={artifact}")
			if profile == "storage-landns" {
				args = append(args, "--set", "config.enabledCapabilities={artifact,dns}", "--set", "config.dnsReadyURL=http://dns-server.dns.svc.cluster.local:8181/ready")
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
	for _, profile := range []string{"landns", "storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			args := append(defaultRenderArgs(),
				"--set", "config.applianceProfile="+profile,
				"--set", "config.enabledCapabilities={dns}",
				"--set", "config.dnsReadyURL=http://dns-server.dns.svc.cluster.local:8181/ready",
			)
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
		})
	}
}

func TestValuesSchemaRejectsArtifactProfilesWithoutArtifactServerURL(t *testing.T) {
	requireHelm(t)
	for _, profile := range []string{"storage", "storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			valuesPath := filepath.Join(t.TempDir(), profile+"-without-artifact-server.yaml")
			values := fmt.Sprintf("config:\n  applianceProfile: %s\n  enabledCapabilities: [artifact]\n  artifactServerBaseURL: \"\"\n", profile)
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
	for _, profile := range []string{"landns", "storage-landns"} {
		t.Run(profile, func(t *testing.T) {
			valuesPath := filepath.Join(t.TempDir(), profile+"-without-dns.yaml")
			values := fmt.Sprintf("config:\n  applianceProfile: %s\n  enabledCapabilities: [dns]\n  dnsReadyURL: \"\"\n", profile)
			switch profile {
			case "storage-landns":
				values += "  artifactServerBaseURL: \"http://appliance-registry.artifacts.svc.cluster.local:5000\"\n  artifactServerAllowFake: false\n"
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

func TestValuesSchemaAllowsCatalogDefinedInferenceProfile(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "catalog-defined-inference.yaml")
	values := []byte(`
config:
  applianceProfile: future-catalog-profile
  enabledCapabilities: [base, inference]
  inferenceGatewayBaseURL: http://inference-gateway.inference.svc.cluster.local:8080
  inferenceRuntimePackage: std-llm
  inferenceEngine: ollama
  inferenceArchitecture: amd64
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("writing test values: %v", err)
	}
	cmd := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helm lint rejected catalog-defined inference profile:\n%s", out)
	}
}

func TestValuesSchemaRequiresInferenceGatewayForCapability(t *testing.T) {
	requireHelm(t)
	valuesPath := filepath.Join(t.TempDir(), "inference-without-gateway.yaml")
	values := []byte(`
config:
  applianceProfile: future-catalog-profile
  enabledCapabilities: [base, inference]
  inferenceGatewayBaseURL: ""
`)
	if err := os.WriteFile(valuesPath, values, 0o600); err != nil {
		t.Fatalf("writing test values: %v", err)
	}
	out, err := exec.Command("helm", "lint", chartDir(t), "-f", valuesPath).CombinedOutput()
	if err == nil {
		t.Fatalf("helm lint accepted inference capability without gateway:\n%s", out)
	}
	if !bytes.Contains(out, []byte("inferenceGatewayBaseURL")) {
		t.Fatalf("helm lint failed for the wrong reason:\n%s", out)
	}
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
	assertNS("Deployment", actionRuntimeName, "ace-apps")

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
	if got, _ := cpData["APPLIANCE_ACTION_RUNTIME_BASE_URL"].(string); got != "http://action-runtime.ace-apps.svc.cluster.local:8082" {
		t.Fatalf("action runtime URL = %q", got)
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

	for _, name := range []string{controlPlaneDeploymentName, controlPlaneUIName, "host-agent", actionRuntimeName} {
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

func containsString(values []any, want string) bool {
	for _, value := range values {
		if got, _ := value.(string); got == want {
			return true
		}
	}
	return false
}

func TestHostPathWorkloadsPinToAdvertisedPrime(t *testing.T) {
	docs := renderChart(t, append(defaultRenderArgs(),
		"--set", "placement.nodeName=192-168-1-151",
		"--set", "hostAgent.enabled=true",
		"--set", "hostAgent.image.reference=registry.local/host-agent@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)...)
	for _, name := range []string{controlPlaneDeploymentName, controlPlaneUIName, "host-agent", actionRuntimeName, "blob-storage"} {
		doc := findByKindAndName(docs, "Deployment", name)
		if doc == nil {
			t.Fatalf("expected Deployment/%s", name)
		}
		if _, ok := at(doc, "spec", "template", "spec", "nodeName").(string); ok {
			t.Fatalf("%s must not set spec.nodeName (bypasses scheduler and blocks WaitForFirstConsumer PVCs)", name)
		}
		selector, _ := at(doc, "spec", "template", "spec", "nodeSelector").(map[string]any)
		if got, _ := selector["kubernetes.io/hostname"].(string); got != "192-168-1-151" {
			t.Fatalf("%s nodeSelector hostname = %q, want advertised prime", name, got)
		}
	}
	foundPVAffinity := 0
	for _, doc := range findByKind(docs, "PersistentVolume") {
		if _, ok := at(doc, "spec", "nodeAffinity").(map[string]any); ok {
			foundPVAffinity++
		}
	}
	if foundPVAffinity < 1 {
		t.Fatal("expected hostPath PVs to pin nodeAffinity to the advertised prime")
	}
}
