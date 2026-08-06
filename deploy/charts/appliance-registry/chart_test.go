package chart

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func chartDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve chart directory")
	}
	return filepath.Dir(file)
}

func render(t *testing.T, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm not installed")
	}
	command := append([]string{"template", "registry", chartDir(t), "--namespace", "control"}, args...)
	out, err := exec.Command("helm", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return string(out)
}

func TestHardenedRegistryRender(t *testing.T) {
	out := render(t, "--set", "logs.prepare.enabled=true", "--set", "networkPolicy.traefikNamespaceLabel.kubernetes\\.io/metadata\\.name=kube-system")
	for _, want := range []string{
		"kind: Deployment\nmetadata:\n  name: artifactserver",
		"runAsUser: 10003", "runAsGroup: 10003", "fsGroup: 20000",
		"readOnlyRootFilesystem: true", "allowPrivilegeEscalation: false",
		"mountPath: /var/lib/registry", "mountPath: /data/zon/logs/artifactserver", "mountPath: /tmp",
		"\"output\": \"/data/zon/logs/artifactserver/application.log\"",
		"accessModes:\n    - ReadWriteOnce", "chmod 2755 /data/zon/logs/artifactserver",
		"chmod 0644 /data/zon/logs/artifactserver/application.log",
		"touch /data/zon/logs/artifactserver/application.log",
		"kind: NetworkPolicy", "name: appliance-registry-default-deny",
		"kubernetes.io/metadata.name: control",
		"app.kubernetes.io/name: api-server",
		"path: /data/zon/logs/artifactserver", "type: DirectoryOrCreate",
		"PathPrefix(`/v2`)", "registry-public.pem", "tcpSocket:",
		"secretName: appliance-registry-verification-key",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
	for _, forbidden := range []string{"name: fileserver", "PathPrefix(`/files`)", "registry.local/fileserver"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("render unexpectedly contains %q", forbidden)
		}
	}
	for _, forbidden := range []string{"path: /v2/", "httpGet:", "ui:", "anonymous", "enableManagement", "scrubInterval", "search:"} {
		if bytes.Contains([]byte(out), []byte(forbidden)) {
			t.Errorf("render unexpectedly contains %q", forbidden)
		}
	}
}

func TestFileserverNeverRenders(t *testing.T) {
	// Leftover installer values must not revive the removed unauthenticated
	// Traefik /files nginx surface.
	out := render(t,
		"--set", "fileserver.enabled=true",
		"--set", "fileserver.image.repository=registry.local/fileserver",
		"--set", "fileserver.image.digest=sha256:"+strings.Repeat("d", 64),
		"--set", "networkPolicy.traefikNamespaceLabel.kubernetes\\.io/metadata\\.name=kube-system",
	)
	for _, forbidden := range []string{
		"name: fileserver", "PathPrefix(`/files`)", "registry.local/fileserver",
		"/data/zon/logs/fileserver", "runAsUser: 10005",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("render unexpectedly contains removed fileserver surface %q", forbidden)
		}
	}
}

func TestImageDigestWins(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	out := render(t, "--set", "image.digest="+digest)
	if !strings.Contains(out, "image: registry.local/artifact-server@"+digest) {
		t.Fatalf("digest-pinned image not rendered")
	}
}

func TestReleaseInputPublishesFirstClassArtifactServerArtifacts(t *testing.T) {
	root := filepath.Clean(filepath.Join(chartDir(t), "..", "..", ".."))
	tmp := t.TempDir()
	for _, name := range []string{"control-plane.tar", "ui.tar"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest := strings.Repeat("a", 64)
	artifactServerLayout := filepath.Join(tmp, "artifact-server-layout")
	if err := os.Mkdir(artifactServerLayout, 0o700); err != nil {
		t.Fatal(err)
	}
	index := `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + digest + `","size":1,"annotations":{"org.opencontainers.image.ref.name":"registry.local/artifact-server:bundled"}}]}`
	if err := os.WriteFile(filepath.Join(artifactServerLayout, "index.json"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	artifactServerArchive := filepath.Join(tmp, "artifact-server.tar")
	if output, err := exec.Command("tar", "-cf", artifactServerArchive, "-C", artifactServerLayout, ".").CombinedOutput(); err != nil {
		t.Fatalf("create Artifact Server archive: %v\n%s", err, output)
	}
	hostDigest := strings.Repeat("d", 64)
	hostLayout := filepath.Join(tmp, "host-layout")
	if err := os.Mkdir(hostLayout, 0o700); err != nil {
		t.Fatal(err)
	}
	hostIndex := `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + hostDigest + `","size":1,"annotations":{"org.opencontainers.image.ref.name":"registry.local/appliance-host-agent:bundled"}}]}`
	if err := os.WriteFile(filepath.Join(hostLayout, "index.json"), []byte(hostIndex), 0o600); err != nil {
		t.Fatal(err)
	}
	hostAgentArchive := filepath.Join(tmp, "host-agent.tar")
	if output, err := exec.Command("tar", "-cf", hostAgentArchive, "-C", hostLayout, ".").CombinedOutput(); err != nil {
		t.Fatalf("create host-agent archive: %v\n%s", err, output)
	}
	hostBinary := filepath.Join(tmp, "appliance-host-agentd")
	if err := os.WriteFile(hostBinary, []byte("host-agentd"), 0o700); err != nil {
		t.Fatal(err)
	}
	hostPackagesRoot := filepath.Join(tmp, "host-packages")
	hostPackagesDir := filepath.Join(hostPackagesRoot, "ubuntu", "24.04", "amd64")
	if err := os.MkdirAll(hostPackagesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostPackagesDir, "avahi-daemon.deb"), []byte("deb"), 0o600); err != nil {
		t.Fatal(err)
	}
	crds := filepath.Join(tmp, "crds")
	if err := os.Mkdir(crds, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crds, "workflow.yaml"), []byte("apiVersion: v1\nkind: List\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(tmp, "release-input.tgz")
	cmd := exec.Command("bash", filepath.Join(root, "scripts/package/archive-release-input.sh"),
		"--out-file", out, "--code-version", "1.2.3", "--k3s-version", "v1.33.0+k3s1",
		"--control-plane-image", filepath.Join(tmp, "control-plane.tar"),
		"--ui-image", filepath.Join(tmp, "ui.tar"),
		"--host-agent-image", hostAgentArchive,
		"--host-agent-image-reference", "registry.local/appliance-host-agent@sha256:"+hostDigest,
		"--host-agent-binary", hostBinary,
		"--host-packages-dir", hostPackagesRoot,
		"--host-packages-os-version", "24.04",
		"--artifact-server-image", artifactServerArchive,
		"--artifact-server-image-reference", "registry.local/artifact-server@sha256:"+digest,
		"--artifact-server-version", "2.1.8", "--workflows-crds-dir", crds)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("archive release input: %v\n%s", err, output)
	}
	extracted := filepath.Join(tmp, "extracted")
	if err := os.Mkdir(extracted, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("tar", "-xzf", out, "-C", extracted).CombinedOutput(); err != nil {
		t.Fatalf("extract: %v\n%s", err, output)
	}
	raw, err := os.ReadFile(filepath.Join(extracted, "release-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Artifacts     map[string]json.RawMessage `json:"artifacts"`
		Compatibility map[string]any             `json:"compatibility"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode release-input.json: %v\n%s", err, raw)
	}
	for _, key := range []string{"hostAgentImage", "hostAgentBinary", "artifactServerImage", "artifactServerChart"} {
		if len(manifest.Artifacts[key]) == 0 {
			t.Errorf("missing first-class %s artifact", key)
		}
	}
	if got := manifest.Compatibility["artifactServerVersion"]; got != "2.1.8" {
		t.Fatalf("artifactServerVersion = %#v", got)
	}
}

func TestReleaseInputRejectsUnpairedArtifactServerImage(t *testing.T) {
	root := filepath.Clean(filepath.Join(chartDir(t), "..", "..", ".."))
	tmp := t.TempDir()
	artifactServer := filepath.Join(tmp, "artifact-server.tar")
	if err := os.WriteFile(artifactServer, []byte("artifact-server"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostDigest := strings.Repeat("d", 64)
	hostLayout := filepath.Join(tmp, "host-layout")
	if err := os.Mkdir(hostLayout, 0o700); err != nil {
		t.Fatal(err)
	}
	hostIndex := `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + hostDigest + `","size":1,"annotations":{"org.opencontainers.image.ref.name":"registry.local/appliance-host-agent:bundled"}}]}`
	if err := os.WriteFile(filepath.Join(hostLayout, "index.json"), []byte(hostIndex), 0o600); err != nil {
		t.Fatal(err)
	}
	hostAgentArchive := filepath.Join(tmp, "host-agent.tar")
	if output, err := exec.Command("tar", "-cf", hostAgentArchive, "-C", hostLayout, ".").CombinedOutput(); err != nil {
		t.Fatalf("create host-agent archive: %v\n%s", err, output)
	}
	hostBinary := filepath.Join(tmp, "appliance-host-agentd")
	if err := os.WriteFile(hostBinary, []byte("host-agentd"), 0o700); err != nil {
		t.Fatal(err)
	}
	hostPackagesRoot := filepath.Join(tmp, "host-packages")
	hostPackagesDir := filepath.Join(hostPackagesRoot, "ubuntu", "24.04", "amd64")
	if err := os.MkdirAll(hostPackagesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostPackagesDir, "avahi-daemon.deb"), []byte("deb"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", filepath.Join(root, "scripts/package/archive-release-input.sh"),
		"--out-file", filepath.Join(tmp, "out.tgz"), "--code-version", "1.2.3",
		"--k3s-version", "v1", "--control-plane-image", artifactServer, "--ui-image", artifactServer,
		"--host-agent-image", hostAgentArchive,
		"--host-agent-image-reference", "registry.local/appliance-host-agent@sha256:"+hostDigest,
		"--host-agent-binary", hostBinary,
		"--host-packages-dir", hostPackagesRoot,
		"--host-packages-os-version", "24.04",
		"--artifact-server-image", artifactServer).CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("must be provided together")) {
		t.Fatalf("unpaired Artifact Server image was not rejected: err=%v output=%s", err, out)
	}
}
