package applications

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
	"strings"
)

const applicationNamespace = "apps"

// KubernetesManager is the in-cluster implementation used by the Control
// Plane. It creates only resources owned by one application in apps.
type KubernetesManager struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewInClusterManager() (*KubernetesManager, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, nil
	}
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, fmt.Errorf("applications: read service account token: %w", err)
	}
	pool := x509.NewCertPool()
	if ca, readErr := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"); readErr == nil {
		pool.AppendCertsFromPEM(ca)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &KubernetesManager{baseURL: "https://" + host + ":" + port, token: strings.TrimSpace(string(token)), client: &http.Client{Transport: transport}}, nil
}

func (m *KubernetesManager) Apply(ctx context.Context, definition Definition) (string, error) {
	port := definition.Runtime.Port
	if port == 0 {
		port = 8080
	}
	labels := map[string]string{"app.kubernetes.io/managed-by": "appliance-control-plane", "appliance.zon/application": definition.Metadata.Name}
	deployment := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"name": definition.Metadata.Name, "namespace": applicationNamespace, "labels": labels},
		"spec": map[string]any{
			"replicas": 1,
			"selector": map[string]any{"matchLabels": labels},
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec": map[string]any{
					"automountServiceAccountToken": false,
					"containers": []any{map[string]any{
						"name":  "application",
						"image": definition.Runtime.Image.Reference,
						"ports": []any{map[string]any{"name": "http", "containerPort": port}},
						"securityContext": map[string]any{
							"allowPrivilegeEscalation": false,
							"runAsNonRoot":             true,
							"readOnlyRootFilesystem":   true,
							"capabilities":             map[string]any{"drop": []string{"ALL"}},
						},
					}},
				},
			},
		},
	}
	service := map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": definition.Metadata.Name, "namespace": applicationNamespace, "labels": labels}, "spec": map[string]any{"selector": labels, "ports": []any{map[string]any{"name": "http", "port": port, "targetPort": port}}}}
	if err := m.apply(ctx, "/apis/apps/v1/namespaces/"+url.PathEscape(applicationNamespace)+"/deployments", definition.Metadata.Name, deployment); err != nil {
		return "error", err
	}
	if err := m.apply(ctx, "/api/v1/namespaces/"+url.PathEscape(applicationNamespace)+"/services", definition.Metadata.Name, service); err != nil {
		return "error", err
	}
	body, status, err := m.do(ctx, http.MethodGet, "/apis/apps/v1/namespaces/"+url.PathEscape(applicationNamespace)+"/deployments/"+url.PathEscape(definition.Metadata.Name), nil, "")
	if err != nil {
		return "error", err
	}
	if status == http.StatusOK {
		var deploymentStatus struct {
			Status struct {
				AvailableReplicas int `json:"availableReplicas"`
			} `json:"status"`
		}
		if err := json.Unmarshal(body, &deploymentStatus); err == nil && deploymentStatus.Status.AvailableReplicas > 0 {
			return "running", nil
		}
	}
	return "pending", nil
}

func (m *KubernetesManager) Delete(ctx context.Context, name string) error {
	for _, path := range []string{"/apis/apps/v1/namespaces/" + url.PathEscape(applicationNamespace) + "/deployments/" + url.PathEscape(name), "/api/v1/namespaces/" + url.PathEscape(applicationNamespace) + "/services/" + url.PathEscape(name)} {
		_, status, err := m.do(ctx, http.MethodDelete, path, nil, "")
		if err != nil {
			return err
		}
		if status != http.StatusOK && status != http.StatusAccepted && status != http.StatusNotFound {
			return fmt.Errorf("kubernetes delete %s returned %d", path, status)
		}
	}
	return nil
}

func (m *KubernetesManager) apply(ctx context.Context, collection, name string, object map[string]any) error {
	body, err := json.Marshal(object)
	if err != nil {
		return err
	}
	_, status, err := m.do(ctx, http.MethodPatch, collection+"/"+url.PathEscape(name), body, "application/apply-patch+yaml")
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		_, _, err = m.do(ctx, http.MethodPost, collection, body, "application/json")
	}
	return err
}

func (m *KubernetesManager) do(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, m.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	if len(body) > 0 {
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	if method == http.MethodPatch {
		req.URL.RawQuery = "fieldManager=appliance-control-plane&force=true"
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return data, resp.StatusCode, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("kubernetes %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, resp.StatusCode, nil
}

var _ ResourceManager = (*KubernetesManager)(nil)
