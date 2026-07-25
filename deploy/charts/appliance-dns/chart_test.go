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
		"--set", "localZone.hostname=appliance",
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
		"kind: Deployment\nmetadata:\n  name: appliance-dns",
		"runAsUser: 10004", "runAsGroup: 10004", "fsGroup: 20000",
		"readOnlyRootFilesystem: true", "allowPrivilegeEscalation: false",
		"NET_BIND_SERVICE", "hostNetwork: true",
		"path: /data/zon/logs/dns", "chmod 2755 /data/zon/logs/dns",
		"kind: NetworkPolicy", "name: appliance-dns-default-deny",
		"file /etc/coredns/zones/db.local", "forward . 1.1.1.1 8.8.8.8",
		"appliance 3600 IN A 192.0.2.10",
		"path: /health", "path: /ready",
		"kind: Namespace",
		"pod-security.kubernetes.io/enforce: privileged",
		"pod-security.kubernetes.io/audit: privileged",
		"pod-security.kubernetes.io/warn: privileged",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
	for _, forbidden := range []string{"chmod 777", "privileged: true"} {
		if bytes.Contains([]byte(out), []byte(forbidden)) {
			t.Errorf("render unexpectedly contains %q", forbidden)
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
	for _, key := range []string{"dnsImage", "dnsChart", "zotImage", "zotChart"} {
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
	out, err := exec.Command("bash", filepath.Join(root, "scripts/package/archive-release-input.sh"),
		"--out-file", filepath.Join(tmp, "out.tgz"), "--code-version", "test",
		"--k3s-version", "v1", "--control-plane-image", dns, "--ui-image", dns,
		"--dns-image", dns).CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("must be provided together")) {
		t.Fatalf("unpaired DNS image was not rejected: err=%v output=%s", err, out)
	}
}
