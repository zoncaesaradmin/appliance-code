package dnsrecords

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

const (
	serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	defaultTimeout    = 30 * time.Second
)

// ZoneSyncer materializes the rendered zone into the CoreDNS data plane.
type ZoneSyncer interface {
	PatchZone(ctx context.Context, zoneFile string) error
}

// MemoryZoneSyncer stores the last patched zone in memory (tests / fake mode).
type MemoryZoneSyncer struct {
	mu   sync.Mutex
	Last string
}

func (m *MemoryZoneSyncer) PatchZone(_ context.Context, zoneFile string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Last = zoneFile
	return nil
}

// ConfigMapZoneSyncer PATCHes the appliance-dns ConfigMap db.local key.
type ConfigMapZoneSyncer struct {
	baseURL   string
	token     string
	client    *http.Client
	namespace string
	name      string
}

// NewInClusterConfigMapZoneSyncer builds a syncer using the pod service account.
func NewInClusterConfigMapZoneSyncer(namespace, name string) (*ConfigMapZoneSyncer, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("dnsrecords: KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT are required for in-cluster zone sync")
	}
	token, err := os.ReadFile(filepath.Join(serviceAccountDir, "token"))
	if err != nil {
		return nil, fmt.Errorf("dnsrecords: read service account token: %w", err)
	}
	caPool := x509.NewCertPool()
	if ca, err := os.ReadFile(filepath.Join(serviceAccountDir, "ca.crt")); err == nil {
		caPool.AppendCertsFromPEM(ca)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: caPool, MinVersion: tls.VersionTLS12}
	return &ConfigMapZoneSyncer{
		baseURL:   "https://" + host + ":" + port,
		token:     strings.TrimSpace(string(token)),
		client:    &http.Client{Transport: transport, Timeout: defaultTimeout},
		namespace: strings.TrimSpace(namespace),
		name:      strings.TrimSpace(name),
	}, nil
}

func (s *ConfigMapZoneSyncer) PatchZone(ctx context.Context, zoneFile string) error {
	if s.namespace == "" || s.name == "" {
		return fmt.Errorf("dnsrecords: configmap namespace and name are required")
	}
	payload := map[string]any{
		"data": map[string]string{
			"db.local": zoneFile,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("dnsrecords: encode configmap patch: %w", err)
	}
	path := "/api/v1/namespaces/" + url.PathEscape(s.namespace) + "/configmaps/" + url.PathEscape(s.name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, s.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	traceCtx, traceID := ctxutil.EnsureTraceID(req.Context())
	req = req.WithContext(traceCtx)
	req.Header.Set(ctxutil.TraceIDHeader, traceID)
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/strategic-merge-patch+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("dnsrecords: patch configmap: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dnsrecords: patch configmap returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}
