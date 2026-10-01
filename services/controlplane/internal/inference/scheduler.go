package inference

// This file contains the control-plane scheduler for node-bound inference
// managers. It deliberately resolves only Kubernetes-owned Services; callers
// can submit a node UID, never an address, Service name, or namespace.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	kubernetesServiceAccountDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	inferenceNodeLabel          = "zon.io/inference-node"
	inferenceNodeUIDLabel       = "zon.io/inference-node-uid"
	inferenceNodeNameLabel      = "zon.io/inference-node-name"
)

// Node is the safe, public scheduling view. Ref is a Kubernetes node UID;
// Name is informational only and must never be supplied as a scheduling key.
type Node struct {
	Ref   string `json:"ref"`
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

// NodeScheduler resolves a node UID to the manager Service selected by the
// appliance controller. It makes no routing decision from user-provided URLs.
type NodeScheduler interface {
	Resolve(context.Context, string) (*url.URL, error)
	List(context.Context) ([]Node, error)
}

// KubernetesScheduler discovers node-bound manager Services and verifies that
// their target node remains an enabled inference node before returning a DNS
// address. It is intentionally built from the pod service-account instead of
// kubeconfig or external cluster credentials.
type KubernetesScheduler struct {
	baseURL   string
	token     string
	client    *http.Client
	namespace string
}

func NewInClusterScheduler(namespace string) (*KubernetesScheduler, error) {
	host, port := strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST")), strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_PORT"))
	if host == "" || port == "" {
		return nil, fmt.Errorf("inference scheduler: Kubernetes service environment is unavailable")
	}
	token, err := os.ReadFile(filepath.Join(kubernetesServiceAccountDir, "token"))
	if err != nil {
		return nil, fmt.Errorf("inference scheduler: read service account token: %w", err)
	}
	pool := x509.NewCertPool()
	if ca, readErr := os.ReadFile(filepath.Join(kubernetesServiceAccountDir, "ca.crt")); readErr == nil {
		pool.AppendCertsFromPEM(ca)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &KubernetesScheduler{
		baseURL: "https://" + host + ":" + port, token: strings.TrimSpace(string(token)),
		client: &http.Client{Transport: transport, Timeout: 10 * time.Second}, namespace: strings.TrimSpace(namespace),
	}, nil
}

func (s *KubernetesScheduler) Resolve(ctx context.Context, ref string) (*url.URL, error) {
	ref = strings.TrimSpace(ref)
	if !validNodeRef(ref) {
		return nil, fmt.Errorf("%w: nodeRef must be a Kubernetes node UID", ErrInvalidRequest)
	}
	service, err := s.managerService(ctx, ref)
	if err != nil {
		return nil, err
	}
	nodeName := service.Metadata.Labels[inferenceNodeNameLabel]
	if err := s.verifyNode(ctx, nodeName, ref); err != nil {
		return nil, err
	}
	serviceName := strings.TrimSpace(service.Metadata.Name)
	port := 8080
	if len(service.Spec.Ports) != 0 && service.Spec.Ports[0].Port > 0 {
		port = service.Spec.Ports[0].Port
	}
	return url.Parse(fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", serviceName, s.namespace, port))
}

func (s *KubernetesScheduler) List(ctx context.Context) ([]Node, error) {
	nodes, err := s.listNodes(ctx)
	if err != nil {
		return nil, err
	}
	services, err := s.listServices(ctx)
	if err != nil {
		return nil, err
	}
	configured := make(map[string]bool, len(services.Items))
	for _, service := range services.Items {
		name := strings.TrimSpace(service.Metadata.Labels[inferenceNodeNameLabel])
		if name != "" {
			configured[name] = true
		}
	}
	result := make([]Node, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		if node.Metadata.Labels[inferenceNodeLabel] != "true" || !configured[node.Metadata.Name] {
			continue
		}
		result = append(result, Node{Ref: node.Metadata.UID, Name: node.Metadata.Name, Ready: nodeReady(node.Status.Conditions)})
	}
	return result, nil
}

func (s *KubernetesScheduler) managerService(ctx context.Context, ref string) (kubeService, error) {
	services, err := s.listServices(ctx, url.Values{"labelSelector": {inferenceNodeUIDLabel + "=" + ref}})
	if err != nil {
		return kubeService{}, err
	}
	if len(services.Items) == 1 {
		return services.Items[0], nil
	}
	if len(services.Items) > 1 {
		return kubeService{}, fmt.Errorf("%w: inference node %q has no unique manager service", ErrUnavailable, ref)
	}
	// The initial appliance manager predates a node UID because it is rendered
	// before K3s starts. Its Service carries a controller-supplied node name;
	// resolve that name through the live Node object and never accept it from a
	// caller.
	nodes, err := s.listNodes(ctx)
	if err != nil {
		return kubeService{}, err
	}
	for _, node := range nodes.Items {
		if node.Metadata.UID != ref {
			continue
		}
		all, err := s.listServices(ctx, url.Values{"labelSelector": {inferenceNodeNameLabel + "=" + node.Metadata.Name}})
		if err != nil {
			return kubeService{}, err
		}
		if len(all.Items) == 1 {
			return all.Items[0], nil
		}
		break
	}
	return kubeService{}, fmt.Errorf("%w: inference node %q has no unique manager service", ErrUnavailable, ref)
}

func (s *KubernetesScheduler) verifyNode(ctx context.Context, name, ref string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: manager service is missing its controller node identity", ErrUnavailable)
	}
	var node kubeNode
	if err := s.get(ctx, "/api/v1/nodes/"+url.PathEscape(name), &node); err != nil {
		return err
	}
	if node.Metadata.UID != ref || node.Metadata.Labels[inferenceNodeLabel] != "true" {
		return fmt.Errorf("%w: inference node %q is no longer registered", ErrUnavailable, ref)
	}
	return nil
}

func (s *KubernetesScheduler) listServices(ctx context.Context, query ...url.Values) (kubeServices, error) {
	path := "/api/v1/namespaces/" + url.PathEscape(s.namespace) + "/services"
	if len(query) != 0 && query[0].Encode() != "" {
		path += "?" + query[0].Encode()
	}
	var result kubeServices
	err := s.get(ctx, path, &result)
	return result, err
}

func (s *KubernetesScheduler) listNodes(ctx context.Context) (kubeNodes, error) {
	var result kubeNodes
	err := s.get(ctx, "/api/v1/nodes?labelSelector="+url.QueryEscape(inferenceNodeLabel+"=true"), &result)
	return result, err
}

func (s *KubernetesScheduler) get(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: query Kubernetes scheduler state: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: Kubernetes scheduler returned %d", ErrUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("%w: decode Kubernetes scheduler response: %v", ErrUnavailable, err)
	}
	return nil
}

type kubeMetadata struct {
	Name   string            `json:"name"`
	UID    string            `json:"uid"`
	Labels map[string]string `json:"labels"`
}
type kubeService struct {
	Metadata kubeMetadata `json:"metadata"`
	Spec     struct {
		Ports []struct {
			Port int `json:"port"`
		} `json:"ports"`
	} `json:"spec"`
}
type kubeServices struct {
	Items []kubeService `json:"items"`
}
type kubeNode struct {
	Metadata kubeMetadata `json:"metadata"`
	Status   struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}
type kubeNodes struct {
	Items []kubeNode `json:"items"`
}

func nodeReady(conditions []struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}) bool {
	for _, condition := range conditions {
		if condition.Type == "Ready" {
			return condition.Status == "True"
		}
	}
	return false
}

func validNodeRef(value string) bool {
	if len(value) == 0 || len(value) > 253 {
		return false
	}
	for _, r := range value {
		if !(r == '-' || r == '.' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}
