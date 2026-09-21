package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultInstanceID        = "default"
	alphaMaxServingInstances = 1
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
	ID                string    `json:"id"`
	RuntimeRef        string    `json:"runtimeRef"`
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
	if m.normalizeAlphaBindingsLocked() {
		return m.saveInstanceRegistryLocked()
	}
	return nil
}

// validateAlphaInstancesLocked makes the current product policy explicit. The
// persistence shape remains instance-oriented so a later, separately validated
// release can raise these limits without another registry migration.
func (m *manager) validateAlphaInstancesLocked() error {
	if len(m.instances.Instances) > alphaMaxServingInstances {
		return fmt.Errorf("unsupported inference layout: Alpha supports one serving instance")
	}
	for id, instance := range m.instances.Instances {
		if id != defaultInstanceID || instance.ID != defaultInstanceID {
			return fmt.Errorf("unsupported inference layout: Alpha supports only the %q instance", defaultInstanceID)
		}
		if len(instance.Models) != alphaModelsPerInstance {
			return fmt.Errorf("unsupported inference layout: Alpha supports one model in the default instance")
		}
		if instance.Replicas != alphaReplicasPerInstance {
			return fmt.Errorf("unsupported inference layout: Alpha supports one replica")
		}
		if modelID := strings.TrimSpace(instance.Models[0]); modelID == "" || !modelRefRE.MatchString(modelID) {
			return fmt.Errorf("invalid default instance model %q", instance.Models[0])
		}
	}
	return nil
}

// normalizeAlphaBindingsLocked removes stale bindings created by earlier
// singleton transitions. Bindings are derived from the one Alpha instance, so
// this is a safe, durable compatibility repair rather than an inference-policy
// decision.
func (m *manager) normalizeAlphaBindingsLocked() bool {
	expected := map[string]modelBinding{}
	if instance, ok := m.instances.Instances[defaultInstanceID]; ok {
		modelID := instance.Models[0]
		expected[modelID] = modelBinding{ModelAlias: modelID, InstanceID: defaultInstanceID, UpdatedAt: instance.UpdatedAt}
	}
	if len(expected) == len(m.instances.Bindings) {
		matches := true
		for modelID := range expected {
			actual, ok := m.instances.Bindings[modelID]
			if !ok || actual.ModelAlias != modelID || actual.InstanceID != defaultInstanceID {
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
	defer m.instanceMu.Unlock()
	if m.instances.Instances == nil {
		m.instances.Instances = map[string]modelInstance{}
	}
	if m.instances.Bindings == nil {
		m.instances.Bindings = map[string]modelBinding{}
	}
	// Loading a different model replaces the Alpha default binding. Do not leave
	// the previously enabled model addressable through stale desired state.
	for alias := range m.instances.Bindings {
		delete(m.instances.Bindings, alias)
	}
	m.instances.Instances[defaultInstanceID] = modelInstance{ID: defaultInstanceID, RuntimeRef: m.engine, ServingProfileRef: "default", Models: []string{modelID}, Replicas: 1, UpdatedAt: now}
	m.instances.Bindings[modelID] = modelBinding{ModelAlias: modelID, InstanceID: defaultInstanceID, UpdatedAt: now}
	if err := m.validateAlphaInstancesLocked(); err != nil {
		return err
	}
	return m.saveInstanceRegistryLocked()
}

func (m *manager) clearDefaultInstance(modelID string) error {
	m.instanceMu.Lock()
	defer m.instanceMu.Unlock()
	instance, ok := m.instances.Instances[defaultInstanceID]
	if !ok || len(instance.Models) != 1 || instance.Models[0] != modelID {
		return nil
	}
	delete(m.instances.Instances, defaultInstanceID)
	if binding, ok := m.instances.Bindings[modelID]; ok && binding.InstanceID == defaultInstanceID {
		delete(m.instances.Bindings, modelID)
	}
	return m.saveInstanceRegistryLocked()
}

// migrateLegacyDefaultInstance carries the old active-model desired state into
// the instance registry. It is idempotent and intentionally does not infer a
// desired instance from a failed or interrupted Load.
func (m *manager) migrateLegacyDefaultInstance() error {
	m.instanceMu.RLock()
	_, exists := m.instances.Instances[defaultInstanceID]
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
	ID       string   `json:"id"`
	Models   []string `json:"models"`
	Replicas int32    `json:"replicas"`
}

func (m *manager) instanceSummaries() []instanceSummary {
	m.instanceMu.RLock()
	defer m.instanceMu.RUnlock()
	items := make([]instanceSummary, 0, len(m.instances.Instances))
	for _, instance := range m.instances.Instances {
		items = append(items, instanceSummary{ID: instance.ID, Models: append([]string(nil), instance.Models...), Replicas: instance.Replicas})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
