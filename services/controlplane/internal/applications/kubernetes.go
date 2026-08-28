package applications

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const applicationNamespace = "apps"
const internalAuthHeader = "X-Appliance-Internal-Auth"

// KubernetesManager is the in-cluster implementation used by the Control
// Plane. It creates only resources owned by one application in apps.
type KubernetesManager struct {
	baseURL string
	token   string
	client  *http.Client
	network NetworkProjector
}

// NetworkProjector is the narrow host-agent boundary used before creating a
// direct ServiceLB listener. A direct endpoint is never treated as available
// when this security projection cannot be applied.
type NetworkProjector interface {
	Apply(ctx context.Context, definition Definition) error
	Withdraw(ctx context.Context, application string) error
}

type SecurityBlockedError struct{ Err error }

func (e SecurityBlockedError) Error() string {
	return "application endpoint security projection is unavailable: " + e.Err.Error()
}
func (e SecurityBlockedError) Unwrap() error { return e.Err }

type hostAgentEndpoint struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
}

type hostAgentPolicy struct {
	Application string              `json:"application"`
	Endpoints   []hostAgentEndpoint `json:"endpoints"`
}

type hostAgentStatus struct {
	Actual  string `json:"actual"`
	Message string `json:"message,omitempty"`
}

type hostAgentMDNSService struct {
	Name        string `json:"name"`
	ServiceType string `json:"serviceType"`
	Port        int    `json:"port"`
}

type hostAgentMDNSRequest struct {
	Application string                 `json:"application"`
	Services    []hostAgentMDNSService `json:"services"`
	Aliases     []string               `json:"aliases,omitempty"`
}

// HostAgentProjector applies only catalog-approved direct endpoints to the
// device-user host agent. The shared token is never available to application
// workloads, and NetworkPolicy permits this call only from the control plane.
type HostAgentProjector struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewHostAgentProjector(baseURL, token string) *HostAgentProjector {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	return &HostAgentProjector{baseURL: baseURL, token: token, client: &http.Client{Timeout: 15 * time.Second}}
}

func (p *HostAgentProjector) Apply(ctx context.Context, definition Definition) error {
	endpoints := directEndpoints(definition)
	if len(endpoints) > 0 {
		if err := p.put(ctx, definition.Metadata.Name, endpoints); err != nil {
			return err
		}
	}
	return p.putMDNS(ctx, definition)
}

func (p *HostAgentProjector) Withdraw(ctx context.Context, application string) error {
	if err := p.put(ctx, application, nil); err != nil {
		return err
	}
	return p.putMDNSRequest(ctx, application, nil, nil)
}

func (p *HostAgentProjector) putMDNS(ctx context.Context, definition Definition) error {
	var services []hostAgentMDNSService
	var aliases []string
	for _, endpoint := range definition.Runtime.Endpoints {
		if endpoint.Direct && endpoint.MDNS != nil {
			services = append(services, hostAgentMDNSService{Name: endpoint.MDNS.Instance, ServiceType: endpoint.MDNS.ServiceType, Port: endpoint.Port})
			if services[len(services)-1].Name == "" {
				services[len(services)-1].Name = definition.Metadata.Name + "-" + endpoint.Name
			}
			aliases = append(aliases, endpoint.MDNS.Aliases...)
		}
	}
	if len(services) == 0 && len(aliases) == 0 {
		return nil
	}
	return p.putMDNSRequest(ctx, definition.Metadata.Name, services, aliases)
}

func (p *HostAgentProjector) putMDNSRequest(ctx context.Context, application string, services []hostAgentMDNSService, aliases ...[]string) error {
	if p == nil {
		return SecurityBlockedError{Err: errors.New("device-user host agent is not configured")}
	}
	var requestedAliases []string
	if len(aliases) > 0 {
		requestedAliases = aliases[0]
	}
	body, err := json.Marshal(hostAgentMDNSRequest{Application: application, Services: services, Aliases: requestedAliases})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.baseURL+"/internal/v1/host/application-mdns/"+url.PathEscape(application), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(internalAuthHeader, p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return SecurityBlockedError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return SecurityBlockedError{Err: fmt.Errorf("host agent mDNS returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))}
	}
	return nil
}

func (p *HostAgentProjector) put(ctx context.Context, application string, endpoints []hostAgentEndpoint) error {
	if p == nil {
		return SecurityBlockedError{Err: errors.New("device-user host agent is not configured")}
	}
	body, err := json.Marshal(hostAgentPolicy{Application: application, Endpoints: endpoints})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.baseURL+"/internal/v1/host/application-firewall/"+url.PathEscape(application), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(internalAuthHeader, p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return SecurityBlockedError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return SecurityBlockedError{Err: fmt.Errorf("host agent returned %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))}
	}
	var status hostAgentStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return SecurityBlockedError{Err: err}
	}
	if status.Actual != "active" && len(endpoints) > 0 {
		return SecurityBlockedError{Err: errors.New(status.Message)}
	}
	return nil
}

// NewInClusterManager returns nil when Kubernetes credentials are deliberately
// unavailable for the active profile. Returning the interface directly avoids
// placing a nil *KubernetesManager inside a non-nil ResourceManager interface.
func NewInClusterManager(network ...NetworkProjector) (ResourceManager, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, nil
	}
	// Profiles without workflows/DNS (core, storage, lanllm, training, …) do not
	// automount a ServiceAccount token. Application reconcile already no-ops when
	// the manager is nil; fail closed only when a token is present but unreadable.
	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("applications: read service account token: %w", err)
	}
	pool := x509.NewCertPool()
	if ca, readErr := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"); readErr == nil {
		pool.AppendCertsFromPEM(ca)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	var projector NetworkProjector
	if len(network) > 0 {
		projector = network[0]
	}
	return &KubernetesManager{baseURL: "https://" + host + ":" + port, token: strings.TrimSpace(string(token)), client: &http.Client{Transport: transport}, network: projector}, nil
}

func (m *KubernetesManager) Apply(ctx context.Context, definition Definition) (string, error) {
	if len(directEndpoints(definition)) > 0 {
		if m.network == nil {
			return "security_blocked", SecurityBlockedError{Err: errors.New("device-user host agent is not configured")}
		}
		if err := m.network.Apply(ctx, definition); err != nil {
			return "security_blocked", err
		}
	}
	port := definition.Runtime.Port
	if port == 0 {
		port = 8080
	}
	labels := map[string]string{"app.kubernetes.io/managed-by": "appliance-control-plane", "appliance.zon/application": definition.Metadata.Name}
	if err := m.applyPersistentVolumes(ctx, definition, labels); err != nil {
		return "error", err
	}
	podSecurity := map[string]any{"runAsNonRoot": true}
	if definition.Runtime.Security.RunAsUser > 0 {
		podSecurity["runAsUser"] = definition.Runtime.Security.RunAsUser
	}
	if definition.Runtime.Security.RunAsGroup > 0 {
		podSecurity["runAsGroup"] = definition.Runtime.Security.RunAsGroup
	}
	if definition.Runtime.Security.FSGroup > 0 {
		podSecurity["fsGroup"] = definition.Runtime.Security.FSGroup
		podSecurity["fsGroupChangePolicy"] = "OnRootMismatch"
	}
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
					"hostNetwork":                  definition.Runtime.Security.HostNetwork,
					"securityContext":              podSecurity,
					"containers": []any{map[string]any{
						"name":  "application",
						"image": definition.Runtime.Image.Reference,
						"ports": containerPorts(definition, port),
						"securityContext": map[string]any{
							"allowPrivilegeEscalation": false,
							"runAsNonRoot":             true,
							"readOnlyRootFilesystem":   true,
							"capabilities":             map[string]any{"drop": []string{"ALL"}},
							"privileged":               definition.Runtime.Security.Privileged,
						},
						"volumeMounts": volumeMounts(definition),
					}},
					"volumes": volumeSources(definition),
				},
			},
		},
	}
	if err := m.apply(ctx, "/apis/apps/v1/namespaces/"+url.PathEscape(applicationNamespace)+"/deployments", definition.Metadata.Name, deployment); err != nil {
		return "error", err
	}
	for _, service := range servicesFor(definition, labels, port) {
		metadata, _ := service["metadata"].(map[string]any)
		name, _ := metadata["name"].(string)
		if err := m.apply(ctx, "/api/v1/namespaces/"+url.PathEscape(applicationNamespace)+"/services", name, service); err != nil {
			return "error", err
		}
	}
	if err := m.apply(ctx, "/apis/networking.k8s.io/v1/namespaces/"+url.PathEscape(applicationNamespace)+"/networkpolicies", definition.Metadata.Name+"-allow", networkPolicyFor(definition, labels)); err != nil {
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

func (m *KubernetesManager) Delete(ctx context.Context, definition Definition) error {
	name := definition.Metadata.Name
	if m.network != nil {
		if err := m.network.Withdraw(ctx, name); err != nil {
			return err
		}
	}
	paths := []string{
		"/apis/apps/v1/namespaces/" + url.PathEscape(applicationNamespace) + "/deployments/" + url.PathEscape(name),
		"/apis/networking.k8s.io/v1/namespaces/" + url.PathEscape(applicationNamespace) + "/networkpolicies/" + url.PathEscape(name+"-allow"),
	}
	for _, service := range servicesFor(definition, nil, definition.Runtime.Port) {
		metadata, _ := service["metadata"].(map[string]any)
		serviceName, _ := metadata["name"].(string)
		paths = append(paths, "/api/v1/namespaces/"+url.PathEscape(applicationNamespace)+"/services/"+url.PathEscape(serviceName))
	}
	for _, path := range paths {
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

func (m *KubernetesManager) applyPersistentVolumes(ctx context.Context, definition Definition, labels map[string]string) error {
	for _, volume := range definition.Runtime.Volumes {
		if volume.Kind != "persistent" {
			continue
		}
		claimName := definition.Metadata.Name + "-" + volume.Name
		claim := map[string]any{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata":   map[string]any{"name": claimName, "namespace": applicationNamespace, "labels": labels},
			"spec": map[string]any{
				"accessModes": []any{"ReadWriteOnce"},
				"resources":   map[string]any{"requests": map[string]any{"storage": volume.Size}},
			},
		}
		if err := m.apply(ctx, "/api/v1/namespaces/"+url.PathEscape(applicationNamespace)+"/persistentvolumeclaims", claimName, claim); err != nil {
			return err
		}
	}
	return nil
}

func volumeMounts(definition Definition) []any {
	if len(definition.Runtime.Volumes) == 0 {
		return nil
	}
	mounts := make([]any, 0, len(definition.Runtime.Volumes))
	for _, volume := range definition.Runtime.Volumes {
		mounts = append(mounts, map[string]any{"name": volume.Name, "mountPath": volume.MountPath, "readOnly": volume.ReadOnly})
	}
	return mounts
}

func volumeSources(definition Definition) []any {
	if len(definition.Runtime.Volumes) == 0 {
		return nil
	}
	volumes := make([]any, 0, len(definition.Runtime.Volumes))
	for _, volume := range definition.Runtime.Volumes {
		source := map[string]any{"name": volume.Name}
		if volume.Kind == "persistent" {
			source["persistentVolumeClaim"] = map[string]any{"claimName": definition.Metadata.Name + "-" + volume.Name}
		} else if volume.Kind == "videoProjection" {
			source["persistentVolumeClaim"] = map[string]any{"claimName": "appliance-video-media", "readOnly": true}
		} else {
			source["emptyDir"] = map[string]any{}
		}
		volumes = append(volumes, source)
	}
	return volumes
}

func networkPolicyFor(definition Definition, labels map[string]string) map[string]any {
	ports := make([]any, 0, len(definition.Runtime.Endpoints))
	for _, endpoint := range definition.Runtime.Endpoints {
		target := endpoint.TargetPort
		if target == 0 {
			target = endpoint.Port
		}
		ports = append(ports, map[string]any{"protocol": endpoint.Protocol, "port": target})
	}
	if len(ports) == 0 {
		ports = append(ports, map[string]any{"protocol": "TCP", "port": definition.Runtime.Port})
	}
	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata":   map[string]any{"name": definition.Metadata.Name + "-allow", "namespace": applicationNamespace, "labels": labels},
		"spec": map[string]any{
			"podSelector": map[string]any{"matchLabels": labels},
			"policyTypes": []any{"Ingress", "Egress"},
			// Host firewall projection is the appliance's external admission
			// boundary. This policy admits traffic only to catalog ports.
			"ingress": []any{map[string]any{"ports": ports}},
			"egress": []any{map[string]any{
				"to": []any{map[string]any{
					"namespaceSelector": map[string]any{
						"matchLabels": map[string]any{"kubernetes.io/metadata.name": "kube-system"},
					},
				}},
				"ports": []any{
					map[string]any{"protocol": "UDP", "port": 53},
					map[string]any{"protocol": "TCP", "port": 53},
				},
			}},
		},
	}
}

func directEndpoints(definition Definition) []hostAgentEndpoint {
	var endpoints []hostAgentEndpoint
	for _, endpoint := range definition.Runtime.Endpoints {
		if endpoint.Direct {
			endpoints = append(endpoints, hostAgentEndpoint{Name: endpoint.Name, Protocol: endpoint.Protocol, Port: endpoint.Port})
		}
	}
	return endpoints
}

func containerPorts(definition Definition, defaultPort int) []any {
	if len(definition.Runtime.Endpoints) == 0 {
		return []any{map[string]any{"name": "http", "containerPort": defaultPort}}
	}
	ports := make([]any, 0, len(definition.Runtime.Endpoints))
	for _, endpoint := range definition.Runtime.Endpoints {
		target := endpoint.TargetPort
		if target == 0 {
			target = endpoint.Port
		}
		ports = append(ports, map[string]any{"name": endpoint.Name, "containerPort": target, "protocol": endpoint.Protocol})
	}
	return ports
}

func servicesFor(definition Definition, labels map[string]string, defaultPort int) []map[string]any {
	if len(definition.Runtime.Endpoints) == 0 {
		return []map[string]any{{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": definition.Metadata.Name, "namespace": applicationNamespace, "labels": labels}, "spec": map[string]any{"selector": labels, "ports": []any{map[string]any{"name": "http", "port": defaultPort, "targetPort": defaultPort}}}}}
	}
	services := make([]map[string]any, 0, len(definition.Runtime.Endpoints))
	for _, endpoint := range definition.Runtime.Endpoints {
		target := endpoint.TargetPort
		if target == 0 {
			target = endpoint.Port
		}
		name := definition.Metadata.Name + "-" + endpoint.Name
		spec := map[string]any{"selector": labels, "ports": []any{map[string]any{"name": endpoint.Name, "protocol": endpoint.Protocol, "port": endpoint.Port, "targetPort": target}}}
		if endpoint.Direct {
			spec["type"] = "LoadBalancer"
			// ServiceLB binds the host port directly; disable accidental NodePort
			// allocation so no second externally reachable port is created.
			spec["allocateLoadBalancerNodePorts"] = false
		}
		services = append(services, map[string]any{"apiVersion": "v1", "kind": "Service", "metadata": map[string]any{"name": name, "namespace": applicationNamespace, "labels": labels}, "spec": spec})
	}
	return services
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
