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
	command := append([]string{
		"template", "dns", chartDir(t), "--namespace", "dns",
		"--set", "image.digest=sha256:" + strings.Repeat("a", 64),
		"--set", "localZone.ipv4=192.0.2.10",
	}, args...)
	out, err := exec.Command("helm", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	return string(out)
}

func TestHardenedDNSRender(t *testing.T) {
	out := render(t, "--set", "logs.prepare.enabled=true")
	for _, want := range []string{
		"kind: Deployment\nmetadata:\n  name: dns-server",
		"runAsUser: 10004", "runAsGroup: 10004", "fsGroup: 20000",
		"readOnlyRootFilesystem: true", "allowPrivilegeEscalation: false",
		"NET_BIND_SERVICE", "hostNetwork: true",
		"path: /data/zon/logs/dns", "chmod 2755 /data/zon/logs/dns",
		"mountPath: /data/zon/logs/dns",
		"kind: NetworkPolicy", "name: dns-server-default-deny",
		"file /etc/coredns/zones/db.local",
		"reload 1s",
		"reload 2s",
		"forward . 1.1.1.1 8.8.8.8",
		"success 9984 30",
		"denial 9984 1 0",
		"ns  3600 IN A 192.0.2.10",
		"path: /health", "path: /ready",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
	if strings.Contains(out, "cache 30\n") {
		t.Error("render must not use bare cache 30; denial TTL must stay short for fast admin upserts")
	}
	if strings.Contains(out, "appliance 3600 IN A") {
		t.Error("default render must not seed a product host A record")
	}
	if strings.Contains(out, "kind: Namespace") {
		t.Error("default render must not own Namespace; zonctl EnsureNamespace creates it")
	}
	for _, forbidden := range []string{
		"chmod 777",
		"privileged: true",
		// Kubernetes rejects pod sysctls when hostNetwork is true.
		"net.ipv4.ip_unprivileged_port_start",
		"sysctls:",
	} {
		if bytes.Contains([]byte(out), []byte(forbidden)) {
			t.Errorf("render unexpectedly contains %q", forbidden)
		}
	}
}

func TestNamespaceCreateRendersPSALabels(t *testing.T) {
	out := render(t, "--set", "namespace.create=true")
	for _, want := range []string{
		"kind: Namespace",
		"pod-security.kubernetes.io/enforce: privileged",
		"pod-security.kubernetes.io/audit: privileged",
		"pod-security.kubernetes.io/warn: privileged",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

func TestImageDigestWins(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	out := render(t, "--set", "image.digest="+digest)
	if !strings.Contains(out, "image: registry.local/coredns@"+digest) {
		t.Fatalf("digest-pinned image not rendered")
	}
}

func TestReleaseInputPublishesFirstClassDNSArtifacts(t *testing.T) {
	root := filepath.Clean(filepath.Join(chartDir(t), "..", "..", ".."))
	tmp := t.TempDir()
	for _, name := range []string{"control-plane.tar", "ui.tar"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest := strings.Repeat("c", 64)
	dnsLayout := filepath.Join(tmp, "dns-layout")
	if err := os.Mkdir(dnsLayout, 0o700); err != nil {
		t.Fatal(err)
	}
	index := `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + digest + `","size":1,"annotations":{"org.opencontainers.image.ref.name":"registry.local/coredns:bundled"}}]}`
	if err := os.WriteFile(filepath.Join(dnsLayout, "index.json"), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	dnsArchive := filepath.Join(tmp, "coredns.tar")
	if output, err := exec.Command("tar", "-cf", dnsArchive, "-C", dnsLayout, ".").CombinedOutput(); err != nil {
		t.Fatalf("create CoreDNS archive: %v\n%s", err, output)
	}
	zotDigest := strings.Repeat("a", 64)
	zotLayout := filepath.Join(tmp, "zot-layout")
	if err := os.Mkdir(zotLayout, 0o700); err != nil {
		t.Fatal(err)
	}
	zotIndex := `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + zotDigest + `","size":1,"annotations":{"org.opencontainers.image.ref.name":"registry.local/zot:bundled"}}]}`
	if err := os.WriteFile(filepath.Join(zotLayout, "index.json"), []byte(zotIndex), 0o600); err != nil {
		t.Fatal(err)
	}
	zotArchive := filepath.Join(tmp, "zot.tar")
	if output, err := exec.Command("tar", "-cf", zotArchive, "-C", zotLayout, ".").CombinedOutput(); err != nil {
		t.Fatalf("create Zot archive: %v\n%s", err, output)
	}
	hostDigest := strings.Repeat("d", 64)
	hostLayout := filepath.Join(tmp, "host-layout")
	if err := os.Mkdir(hostLayout, 0o700); err != nil {
		t.Fatal(err)
	}
	hostIndex := `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + hostDigest + `","size":1,"annotations":{"org.opencontainers.image.ref.name":"registry.local/appliance-host-service:bundled"}}]}`
	if err := os.WriteFile(filepath.Join(hostLayout, "index.json"), []byte(hostIndex), 0o600); err != nil {
		t.Fatal(err)
	}
	hostArchive := filepath.Join(tmp, "host-service.tar")
	if output, err := exec.Command("tar", "-cf", hostArchive, "-C", hostLayout, ".").CombinedOutput(); err != nil {
		t.Fatalf("create host-service archive: %v\n%s", err, output)
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
		"--out-file", out, "--code-version", "test", "--k3s-version", "v1.33.0+k3s1",
		"--control-plane-image", filepath.Join(tmp, "control-plane.tar"),
		"--ui-image", filepath.Join(tmp, "ui.tar"),
		"--host-service-image", hostArchive,
		"--host-service-image-reference", "registry.local/appliance-host-service@sha256:"+hostDigest,
		"--zot-image", zotArchive,
		"--zot-image-reference", "registry.local/zot@sha256:"+zotDigest,
		"--zot-version", "2.1.8",
		"--dns-image", dnsArchive,
		"--dns-image-reference", "registry.local/coredns@sha256:"+digest,
		"--dns-version", "1.14.4",
		"--argo-crds-dir", crds)
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
	for _, key := range []string{"hostServiceImage", "dnsImage", "dnsChart", "zotImage", "zotChart"} {
		if len(manifest.Artifacts[key]) == 0 {
			t.Errorf("missing first-class %s artifact", key)
		}
	}
	if got := manifest.Compatibility["dnsVersion"]; got != "1.14.4" {
		t.Fatalf("dnsVersion = %#v", got)
	}
}

func TestReleaseInputRejectsUnpairedDNSImage(t *testing.T) {
	root := filepath.Clean(filepath.Join(chartDir(t), "..", "..", ".."))
	tmp := t.TempDir()
	dns := filepath.Join(tmp, "dns.tar")
	if err := os.WriteFile(dns, []byte("dns"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostDigest := strings.Repeat("d", 64)
	hostLayout := filepath.Join(tmp, "host-layout")
	if err := os.Mkdir(hostLayout, 0o700); err != nil {
		t.Fatal(err)
	}
	hostIndex := `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:` + hostDigest + `","size":1,"annotations":{"org.opencontainers.image.ref.name":"registry.local/appliance-host-service:bundled"}}]}`
	if err := os.WriteFile(filepath.Join(hostLayout, "index.json"), []byte(hostIndex), 0o600); err != nil {
		t.Fatal(err)
	}
	hostArchive := filepath.Join(tmp, "host-service.tar")
	if output, err := exec.Command("tar", "-cf", hostArchive, "-C", hostLayout, ".").CombinedOutput(); err != nil {
		t.Fatalf("create host-service archive: %v\n%s", err, output)
	}
	out, err := exec.Command("bash", filepath.Join(root, "scripts/package/archive-release-input.sh"),
		"--out-file", filepath.Join(tmp, "out.tgz"), "--code-version", "test",
		"--k3s-version", "v1", "--control-plane-image", dns, "--ui-image", dns,
		"--host-service-image", hostArchive,
		"--host-service-image-reference", "registry.local/appliance-host-service@sha256:"+hostDigest,
		"--dns-image", dns).CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("must be provided together")) {
		t.Fatalf("unpaired DNS image was not rejected: err=%v output=%s", err, out)
	}
}
