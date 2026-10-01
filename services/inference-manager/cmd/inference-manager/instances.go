package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultInstanceID        = "default"
	alphaModelsPerInstance   = 1
	alphaReplicasPerInstance = 1
)

// modelInstance is desired serving state. Kubernetes resource names and
// runtime-local paths are deliberately excluded: they are controller details,
// not durable appliance API identifiers.
//
// The current engine reconciler supports exactly one model in the default
// instance. Keeping Models as a slice now makes that limitation explicit and
// lets a later runtime capability declaration safely enable multi-model
// instances without another persistence migration.
type modelInstance struct {
	ID string `json:"id"`
	// NodeRef is the Kubernetes node identity selected by the appliance
	// controller. It is persisted with desired state so a later multi-node
	// reconciler can enforce one active model instance per node.
	NodeRef           string    `json:"nodeRef"`
	RuntimeRef        string    `json:"runtimeRef"`
	EndpointRef       string    `json:"endpointRef"`
	ServingProfileRef string    `json:"servingProfileRef"`
	Models            []string  `json:"models"`
	Replicas          int32     `json:"replicas"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type modelBinding struct {
	ModelAlias string    `json:"modelAlias"`
	InstanceID string    `json:"instanceId"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type instanceRegistry struct {
	Instances map[string]modelInstance `json:"instances"`
	Bindings  map[string]modelBinding  `json:"bindings"`
}

func (m *manager) instanceRegistryPath() string {
	return filepath.Join(m.modelsDir, ".zon", "instances.json")
}

func (m *manager) loadInstanceRegistry() error {
	m.instanceMu.Lock()
	defer m.instanceMu.Unlock()
	m.instances = instanceRegistry{Instances: map[string]modelInstance{}, Bindings: map[string]modelBinding{}}
	data, err := os.ReadFile(m.instanceRegistryPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &m.instances); err != nil {
		return fmt.Errorf("decode instance registry: %w", err)
	}
	if m.instances.Instances == nil {
		m.instances.Instances = map[string]modelInstance{}
	}
	if m.instances.Bindings == nil {
		m.instances.Bindings = map[string]modelBinding{}
	}
	if err := m.validateAlphaInstancesLocked(); err != nil {
		return err
	}
	// Older singleton registries predate node-bound desired state. They are
	// local by definition, so migrate them without inventing a remote node.
	updated := false
	for id, instance := range m.instances.Instances {
		if strings.TrimSpace(instance.NodeRef) == "" {
			instance.NodeRef = m.instanceNodeID()
			m.instances.Instances[id] = instance
			updated = true
		}
	}
	// The former singleton key was "default". A shared cluster registry needs
	// globally distinct IDs, so move it to the node-derived identity during the
	// first manager restart after this migration.
	if legacy, ok := m.instances.Instances[defaultInstanceID]; ok {
		localID := m.localInstanceID()
		if localID != defaultInstanceID && legacy.NodeRef == m.instanceNodeID() {
			delete(m.instances.Instances, defaultInstanceID)
			legacy.ID = localID
			m.instances.Instances[localID] = legacy
			for alias, binding := range m.instances.Bindings {
				if binding.InstanceID == defaultInstanceID {
					binding.InstanceID = localID
					m.instances.Bindings[alias] = binding
				}
			}
			updated = true
		}
	}
	if updated {
		return m.saveInstanceRegistryLocked()
	}
	if m.normalizeAlphaBindingsLocked() {
		return m.saveInstanceRegistryLocked()
	}
	return nil
}

func (m *manager) instanceNodeID() string {
	if nodeID := strings.TrimSpace(m.nodeID); nodeID != "" {
		return nodeID
	}
	return "local"
}

func (m *manager) localInstanceID() string {
	nodeID := strings.TrimSpace(m.nodeID)
	if nodeID == "" || nodeID == "local" {
		return defaultInstanceID
	}
	return "node-" + nodeID
}

// validateAlphaInstancesLocked enforces the current hardware policy: one
// model/replica per appliance-managed node. The registry is cluster-capable;
// the local reconciler only acts on its own NodeRef.
func (m *manager) validateAlphaInstancesLocked() error {
	nodes := map[string]string{}
	models := map[string]string{}
	for id, instance := range m.instances.Instances {
		if id == "" || instance.ID != id {
			return fmt.Errorf("invalid inference instance ID %q", id)
		}
		if node := strings.TrimSpace(instance.NodeRef); node != "" {
			if prior, exists := nodes[node]; exists {
				return fmt.Errorf("unsupported inference layout: instances %q and %q target node %q", prior, id, node)
			}
			nodes[node] = id
		}
		if len(instance.Models) != alphaModelsPerInstance {
			return fmt.Errorf("unsupported inference layout: one model per instance is required")
		}
		if instance.Replicas != alphaReplicasPerInstance {
			return fmt.Errorf("unsupported inference layout: Alpha supports one replica")
		}
		if modelID := strings.TrimSpace(instance.Models[0]); modelID == "" || !modelRefRE.MatchString(modelID) {
			return fmt.Errorf("invalid instance model %q", instance.Models[0])
		} else if prior, exists := models[modelID]; exists {
			return fmt.Errorf("unsupported inference layout: model %q is bound by both %q and %q", modelID, prior, id)
		} else {
			models[modelID] = id
		}
	}
	return nil
}

// normalizeAlphaBindingsLocked derives aliases from the cluster instance
// registry, removing stale routes after a model moves between nodes.
func (m *manager) normalizeAlphaBindingsLocked() bool {
	expected := map[string]modelBinding{}
	for id, instance := range m.instances.Instances {
		if len(instance.Models) == 1 {
			modelID := instance.Models[0]
			expected[modelID] = modelBinding{ModelAlias: modelID, InstanceID: id, UpdatedAt: instance.UpdatedAt}
		}
	}
	if len(expected) == len(m.instances.Bindings) {
		matches := true
		for modelID, expectedBinding := range expected {
			actual, ok := m.instances.Bindings[modelID]
			if !ok || actual.ModelAlias != modelID || actual.InstanceID != expectedBinding.InstanceID {
				matches = false
				break
			}
		}
		if matches {
			return false
		}
	}
	m.instances.Bindings = expected
	return true
}

func (m *manager) saveInstanceRegistryLocked() error {
	path := m.instanceRegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(m.instances, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "instances-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o660); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(payload, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (m *manager) setDefaultInstance(modelID string) error {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || !modelRefRE.MatchString(modelID) {
		return fmt.Errorf("invalid default instance model %q", modelID)
	}
	now := time.Now().UTC()
	m.instanceMu.Lock()
	if m.instances.Instances == nil {
		m.instances.Instances = map[string]modelInstance{}
	}
	if m.instances.Bindings == nil {
		m.instances.Bindings = map[string]modelBinding{}
	}
	// Loading a different local model replaces only this manager's binding;
	// another node's routed aliases remain intact.
	localID := m.localInstanceID()
	for alias, binding := range m.instances.Bindings {
		if binding.InstanceID == localID {
			delete(m.instances.Bindings, alias)
		}
	}
	// Route through a manager endpoint, not the engine Service. A remote manager
	// must retain its own readiness, compatibility, and identity enforcement.
	m.instances.Instances[localID] = modelInstance{ID: localID, NodeRef: m.instanceNodeID(), RuntimeRef: m.engine, EndpointRef: env("INFERENCE_MANAGER_ENDPOINT", ""), ServingProfileRef: "default", Models: []string{modelID}, Replicas: 1, UpdatedAt: now}
	m.instances.Bindings[modelID] = modelBinding{ModelAlias: modelID, InstanceID: localID, UpdatedAt: now}
	if err := m.validateAlphaInstancesLocked(); err != nil {
		m.instanceMu.Unlock()
		return err
	}
	if err := m.saveInstanceRegistryLocked(); err != nil {
		m.instanceMu.Unlock()
		return err
	}
	instance := m.instances.Instances[localID]
	m.instanceMu.Unlock()
	if m.routingStore == nil {
		return nil
	}
	registry, err := m.routingStore.Upsert(context.Background(), instance)
	if err != nil {
		return fmt.Errorf("publish cluster inference instance: %w", err)
	}
	return m.replaceInstanceRegistry(registry)
}

func (m *manager) clearDefaultInstance(modelID string) error {
	m.instanceMu.Lock()
	localID := m.localInstanceID()
	instance, ok := m.instances.Instances[localID]
	if !ok || len(instance.Models) != 1 || instance.Models[0] != modelID {
		m.instanceMu.Unlock()
		return nil
	}
	delete(m.instances.Instances, localID)
	if binding, ok := m.instances.Bindings[modelID]; ok && binding.InstanceID == localID {
		delete(m.instances.Bindings, modelID)
	}
	if err := m.saveInstanceRegistryLocked(); err != nil {
		m.instanceMu.Unlock()
		return err
	}
	m.instanceMu.Unlock()
	if m.routingStore == nil {
		return nil
	}
	registry, err := m.routingStore.Remove(context.Background(), localID)
	if err != nil {
		return fmt.Errorf("remove cluster inference instance: %w", err)
	}
	return m.replaceInstanceRegistry(registry)
}

// replaceInstanceRegistry atomically updates the local restart cache from a
// ConfigMap result. The ConfigMap contains every node's aliases, allowing any
// manager to list and proxy every currently published model.
func (m *manager) replaceInstanceRegistry(registry instanceRegistry) error {
	if err := rebuildRoutingBindings(&registry); err != nil {
		return err
	}
	m.instanceMu.Lock()
	defer m.instanceMu.Unlock()
	m.instances = registry
	return m.saveInstanceRegistryLocked()
}

// refreshRoutingRegistry imports other nodes' routes. It never republishes a
// local cache entry: publication happens only after local engine readiness,
// which prevents a restarted node from advertising a stale model.
func (m *manager) refreshRoutingRegistry(ctx context.Context) error {
	if m.routingStore == nil {
		return nil
	}
	registry, err := m.routingStore.Load(ctx)
	if err != nil {
		return fmt.Errorf("read cluster inference registry: %w", err)
	}
	return m.replaceInstanceRegistry(registry)
}

// syncLocalRoutingRegistry restores a ready local instance after a manager
// restart, then imports the complete ConfigMap snapshot. A cached instance is
// deliberately not republished unless the local load state says it was ready;
// this avoids resurrecting a model after an interrupted load or node failure.
func (m *manager) syncLocalRoutingRegistry(ctx context.Context) error {
	if m.routingStore == nil {
		return nil
	}
	load := m.currentLoadProgress()
	m.instanceMu.RLock()
	local, exists := m.instances.Instances[m.localInstanceID()]
	m.instanceMu.RUnlock()
	if exists && load.State == "ready" && len(local.Models) == 1 && local.Models[0] == load.ModelID {
		registry, err := m.routingStore.Upsert(ctx, local)
		if err != nil {
			return fmt.Errorf("republish ready local inference instance: %w", err)
		}
		return m.replaceInstanceRegistry(registry)
	}
	return m.refreshRoutingRegistry(ctx)
}

func (m *manager) startRoutingRegistryRefresh(ctx context.Context) {
	if m.routingStore == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.refreshRoutingRegistry(ctx); err != nil {
					log.Printf("refresh cluster inference registry: %v", err)
				}
			}
		}
	}()
}

// migrateLegacyDefaultInstance carries the old active-model desired state into
// the instance registry. It is idempotent and intentionally does not infer a
// desired instance from a failed or interrupted Load.
func (m *manager) migrateLegacyDefaultInstance() error {
	m.instanceMu.RLock()
	_, exists := m.instances.Instances[m.localInstanceID()]
	m.instanceMu.RUnlock()
	if exists {
		return nil
	}
	load := m.currentLoadProgress()
	if load.State != "ready" || load.ModelID == "" {
		return nil
	}
	return m.setDefaultInstance(load.ModelID)
}

type instanceSummary struct {
	ID          string   `json:"id"`
	NodeRef     string   `json:"nodeRef,omitempty"`
	RuntimeRef  string   `json:"runtimeRef,omitempty"`
	EndpointRef string   `json:"endpointRef,omitempty"`
	Models      []string `json:"models"`
	Replicas    int32    `json:"replicas"`
}

func (m *manager) instanceSummaries() []instanceSummary {
	m.instanceMu.RLock()
	defer m.instanceMu.RUnlock()
	items := make([]instanceSummary, 0, len(m.instances.Instances))
	for _, instance := range m.instances.Instances {
		items = append(items, instanceSummary{ID: instance.ID, NodeRef: instance.NodeRef, RuntimeRef: instance.RuntimeRef, EndpointRef: instance.EndpointRef, Models: append([]string(nil), instance.Models...), Replicas: instance.Replicas})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (m *manager) instanceIDForModel(modelID string) string {
	m.instanceMu.RLock()
	defer m.instanceMu.RUnlock()
	if binding, ok := m.instances.Bindings[modelID]; ok {
		return binding.InstanceID
	}
	return ""
}

// instanceForModel resolves an appliance-managed public model alias to its
// serving instance. The registry is the sole routing authority: callers must
// never be allowed to nominate an arbitrary upstream URL in an OpenAI request.
func (m *manager) instanceForModel(modelID string) (modelInstance, bool) {
	m.instanceMu.RLock()
	defer m.instanceMu.RUnlock()
	binding, ok := m.instances.Bindings[modelID]
	if !ok {
		return modelInstance{}, false
	}
	instance, ok := m.instances.Instances[binding.InstanceID]
	return instance, ok
}

func (m *manager) routedModelIDs() []string {
	m.instanceMu.RLock()
	defer m.instanceMu.RUnlock()
	ids := make([]string, 0, len(m.instances.Bindings))
	for modelID := range m.instances.Bindings {
		ids = append(ids, modelID)
	}
	sort.Strings(ids)
	return ids
}

func (m *manager) hasRoutedModels() bool {
	m.instanceMu.RLock()
	defer m.instanceMu.RUnlock()
	return len(m.instances.Bindings) > 0
}
