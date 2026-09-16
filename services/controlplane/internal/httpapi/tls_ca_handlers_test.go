package httpapi_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"appliance-code/services/controlplane/internal/httpapi"
)

func writeTempCAPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"Zon Appliance"}, CommonName: "Zon Appliance CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o440); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	return path
}

func TestDownloadCAServesPEMAttachment(t *testing.T) {
	path := writeTempCAPEM(t)
	h := &httpapi.IdentityHandlers{CACertPath: path}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/appliance/tls/ca", h.DownloadCA)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/appliance/tls/ca")
	if err != nil {
		t.Fatalf("GET ca: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/x-pem-file" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "appliance-ca.pem") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("body is not a PEM certificate: %q", body)
	}
}

func TestDownloadCAUnavailableWhenMissing(t *testing.T) {
	h := &httpapi.IdentityHandlers{CACertPath: filepath.Join(t.TempDir(), "missing.crt")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/appliance/tls/ca", h.DownloadCA)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/appliance/tls/ca")
	if err != nil {
		t.Fatalf("GET ca: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
