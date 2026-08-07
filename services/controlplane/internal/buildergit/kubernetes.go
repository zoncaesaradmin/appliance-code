package buildergit

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
	"time"

	"github.com/zoncaesaradmin/platformkit/ctxutil"
)

const (
	serviceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	defaultTimeout    = 30 * time.Second
)

type KubernetesSecretManager struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewInClusterSecretManager() (*KubernetesSecretManager, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("buildergit: KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT are required for in-cluster mode")
	}
	token, err := os.ReadFile(filepath.Join(serviceAccountDir, "token"))
	if err != nil {
		return nil, fmt.Errorf("buildergit: read service account token: %w", err)
	}
	caPool := x509.NewCertPool()
	if ca, err := os.ReadFile(filepath.Join(serviceAccountDir, "ca.crt")); err == nil {
		caPool.AppendCertsFromPEM(ca)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: caPool, MinVersion: tls.VersionTLS12}
	return &KubernetesSecretManager{
		baseURL: "https://" + host + ":" + port,
		token:   strings.TrimSpace(string(token)),
		client:  &http.Client{Transport: transport, Timeout: defaultTimeout},
	}, nil
}

func (m *KubernetesSecretManager) Get(ctx context.Context, namespace, name string) (Secret, bool, error) {
	body, status, err := m.do(ctx, http.MethodGet, secretPath(namespace, name), nil)
	if err != nil {
		return Secret{}, false, err
	}
	if status == http.StatusNotFound {
		return Secret{}, false, nil
	}
	secret, err := decodeKubernetesSecret(body)
	if err != nil {
		return Secret{}, false, err
	}
	return secret, true, nil
}

func (m *KubernetesSecretManager) List(ctx context.Context, namespace, labelSelector string) ([]Secret, error) {
	path := secretsPath(namespace)
	if strings.TrimSpace(labelSelector) != "" {
		path += "?labelSelector=" + url.QueryEscape(labelSelector)
	}
	body, status, err := m.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	var payload struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("buildergit: decode Kubernetes secret list: %w", err)
	}
	out := make([]Secret, 0, len(payload.Items))
	for _, item := range payload.Items {
		secret, err := decodeKubernetesSecret(item)
		if err != nil {
			return nil, err
		}
		out = append(out, secret)
	}
	return out, nil
}

func (m *KubernetesSecretManager) Upsert(ctx context.Context, namespace, name string, secret Secret) error {
	metadata := map[string]any{
		"name":      name,
		"namespace": namespace,
	}
	if secret.ResourceVersion != "" {
		metadata["resourceVersion"] = secret.ResourceVersion
	}
	if len(secret.Labels) > 0 {
		metadata["labels"] = cloneMap(secret.Labels)
	}
	payload := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   metadata,
		"type":       "Opaque",
		"data":       map[string]string{},
	}
	for key, value := range secret.Data {
		payload["data"].(map[string]string)[key] = EncodeSecretValue(value)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("buildergit: encode Kubernetes secret: %w", err)
	}
	method := http.MethodPost
	path := secretsPath(namespace)
	if secret.ResourceVersion != "" {
		method = http.MethodPut
		path = secretPath(namespace, name)
	}
	_, _, err = m.do(ctx, method, path, body)
	return err
}

func (m *KubernetesSecretManager) Delete(ctx context.Context, namespace, name string) error {
	_, status, err := m.do(ctx, http.MethodDelete, secretPath(namespace, name), nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	return nil
}

func decodeKubernetesSecret(body []byte) (Secret, error) {
	var payload struct {
		Metadata struct {
			Name            string            `json:"name"`
			ResourceVersion string            `json:"resourceVersion"`
			Labels          map[string]string `json:"labels"`
		} `json:"metadata"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Secret{}, fmt.Errorf("buildergit: decode Kubernetes secret: %w", err)
	}
	secret := Secret{
		Name:            payload.Metadata.Name,
		ResourceVersion: payload.Metadata.ResourceVersion,
		Labels:          cloneMap(payload.Metadata.Labels),
		Data:            map[string]string{},
	}
	for key, value := range payload.Data {
		decoded, err := decodeSecretValue(value)
		if err != nil {
			return Secret{}, fmt.Errorf("buildergit: decode secret key %q: %w", key, err)
		}
		secret.Data[key] = decoded
	}
	return secret, nil
}

func (m *KubernetesSecretManager) do(ctx context.Context, method, path string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	traceCtx, traceID := ctxutil.EnsureTraceID(req.Context())
	req = req.WithContext(traceCtx)
	req.Header.Set(ctxutil.TraceIDHeader, traceID)
	req.Header.Set("Authorization", "Bearer "+m.token)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("buildergit: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return respBody, resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("buildergit: %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, resp.StatusCode, nil
}

func secretsPath(namespace string) string {
	return "/api/v1/namespaces/" + url.PathEscape(namespace) + "/secrets"
}

func secretPath(namespace, name string) string {
	return secretsPath(namespace) + "/" + url.PathEscape(name)
}
