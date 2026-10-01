package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
)

const routingRegistryDataKey = "instances.json"

// routingRegistryStore is the cluster-owned desired-state store for ready
// inference instances. The local PVC file is a restart cache only when this
// store is configured.
type routingRegistryStore interface {
	Load(context.Context) (instanceRegistry, error)
	Upsert(context.Context, modelInstance) (instanceRegistry, error)
	Remove(context.Context, string) (instanceRegistry, error)
}

type configMapRoutingRegistry struct {
	client    kubernetes.Interface
	namespace string
	name      string
}

// newRoutingRegistryFromEnv returns both the shared registry and the immutable
// Kubernetes node UID for this manager.  INFERENCE_NODE_ID is intentionally a
// downward-API node *name*, so it is never suitable as durable routing
// identity: a replacement node may reuse the name.  Even the bootstrap
// release therefore looks up its UID through the authenticated Kubernetes API.
func newRoutingRegistryFromEnv() (routingRegistryStore, string, error) {
	name := strings.TrimSpace(os.Getenv("INFERENCE_ROUTING_CONFIGMAP"))
	if name == "" {
		return nil, "", nil
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, "", fmt.Errorf("routing registry requires in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, "", err
	}
	nodeUID, err := routingNodeUID(context.Background(), client, env("INFERENCE_NODE_NAME", env("INFERENCE_NODE_ID", "")))
	if err != nil {
		return nil, "", err
	}
	return &configMapRoutingRegistry{client: client, namespace: env("INFERENCE_NAMESPACE", "inference"), name: name}, nodeUID, nil
}

func routingNodeUID(ctx context.Context, client kubernetes.Interface, nodeName string) (string, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return "", fmt.Errorf("routing registry requires this pod's Kubernetes node name")
	}
	node, err := client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("routing registry resolve node %q: %w", nodeName, err)
	}
	uid := strings.TrimSpace(string(node.UID))
	if uid == "" {
		return "", fmt.Errorf("routing registry node %q has no UID", nodeName)
	}
	return uid, nil
}

func emptyInstanceRegistry() instanceRegistry {
	return instanceRegistry{Instances: map[string]modelInstance{}, Bindings: map[string]modelBinding{}}
}

func decodeRoutingRegistry(data map[string]string) (instanceRegistry, error) {
	registry := emptyInstanceRegistry()
	raw := strings.TrimSpace(data[routingRegistryDataKey])
	if raw == "" {
		return registry, nil
	}
	if err := json.Unmarshal([]byte(raw), &registry); err != nil {
		return instanceRegistry{}, fmt.Errorf("decode routing registry: %w", err)
	}
	if registry.Instances == nil {
		registry.Instances = map[string]modelInstance{}
	}
	if registry.Bindings == nil {
		registry.Bindings = map[string]modelBinding{}
	}
	// Bindings are derived data. Rebuilding them also rejects a corrupted or
	// manually edited ConfigMap before it becomes a routing decision.
	if err := rebuildRoutingBindings(&registry); err != nil {
		return instanceRegistry{}, err
	}
	return registry, nil
}

func encodeRoutingRegistry(registry instanceRegistry) (string, error) {
	raw, err := json.Marshal(registry)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func rebuildRoutingBindings(registry *instanceRegistry) error {
	bindings := map[string]modelBinding{}
	nodes := map[string]string{}
	for id, instance := range registry.Instances {
		if id == "" || instance.ID != id || strings.TrimSpace(instance.NodeRef) == "" || len(instance.Models) != 1 || instance.Replicas != 1 {
			return fmt.Errorf("invalid cluster inference instance %q", id)
		}
		if prior, exists := nodes[instance.NodeRef]; exists && prior != id {
			return fmt.Errorf("cluster inference instances %q and %q target node %q", prior, id, instance.NodeRef)
		}
		nodes[instance.NodeRef] = id
		modelID := strings.TrimSpace(instance.Models[0])
		if !modelRefRE.MatchString(modelID) {
			return fmt.Errorf("invalid cluster inference model %q", modelID)
		}
		if prior, exists := bindings[modelID]; exists && prior.InstanceID != id {
			return fmt.Errorf("cluster inference model %q is served by multiple nodes", modelID)
		}
		if instance.RuntimeRef != "ollama" && instance.RuntimeRef != "vllm" {
			return fmt.Errorf("cluster inference instance %q has unsupported runtime %q", id, instance.RuntimeRef)
		}
		if _, err := applianceEndpointURL(instance.EndpointRef); err != nil {
			return fmt.Errorf("cluster inference instance %q has invalid serving endpoint: %w", id, err)
		}
		bindings[modelID] = modelBinding{ModelAlias: modelID, InstanceID: id, UpdatedAt: instance.UpdatedAt}
	}
	registry.Bindings = bindings
	return nil
}

func (s *configMapRoutingRegistry) Load(ctx context.Context) (instanceRegistry, error) {
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return emptyInstanceRegistry(), nil
	}
	if err != nil {
		return instanceRegistry{}, err
	}
	return decodeRoutingRegistry(cm.Data)
}

func (s *configMapRoutingRegistry) Upsert(ctx context.Context, instance modelInstance) (result instanceRegistry, err error) {
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, getErr := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			registry := emptyInstanceRegistry()
			registry.Instances[instance.ID] = instance
			if err := rebuildRoutingBindings(&registry); err != nil {
				return err
			}
			payload, err := encodeRoutingRegistry(registry)
			if err != nil {
				return err
			}
			_, err = s.client.CoreV1().ConfigMaps(s.namespace).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: s.name, Namespace: s.namespace, Labels: map[string]string{"app.kubernetes.io/part-of": "appliance-inference"}}, Data: map[string]string{routingRegistryDataKey: payload}}, metav1.CreateOptions{})
			if err == nil {
				result = registry
			}
			return err
		}
		if getErr != nil {
			return getErr
		}
		registry, err := decodeRoutingRegistry(cm.Data)
		if err != nil {
			return err
		}
		for id, current := range registry.Instances {
			if current.NodeRef == instance.NodeRef && id != instance.ID {
				return fmt.Errorf("node %q already has inference instance %q", instance.NodeRef, id)
			}
		}
		registry.Instances[instance.ID] = instance
		if err := rebuildRoutingBindings(&registry); err != nil {
			return err
		}
		payload, err := encodeRoutingRegistry(registry)
		if err != nil {
			return err
		}
		if cm.Data == nil {
			cm.Data = map[string]string{}
		}
		cm.Data[routingRegistryDataKey] = payload
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		if err == nil {
			result = registry
		}
		return err
	})
	return result, err
}

func (s *configMapRoutingRegistry) Remove(ctx context.Context, instanceID string) (result instanceRegistry, err error) {
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, s.name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			result = emptyInstanceRegistry()
			return nil
		}
		if err != nil {
			return err
		}
		registry, err := decodeRoutingRegistry(cm.Data)
		if err != nil {
			return err
		}
		delete(registry.Instances, instanceID)
		if err := rebuildRoutingBindings(&registry); err != nil {
			return err
		}
		payload, err := encodeRoutingRegistry(registry)
		if err != nil {
			return err
		}
		cm.Data[routingRegistryDataKey] = payload
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		if err == nil {
			result = registry
		}
		return err
	})
	return result, err
}
