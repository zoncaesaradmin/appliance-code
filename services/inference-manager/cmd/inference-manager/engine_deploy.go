package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Resource accounting (single contract for catalog eligibility and Load):
//
//	requiredBytes = modelEstimateBytes + shmBytes + podMarginBytes
//	availableBytes = host usable memory (MemAvailable*0.75, or GPU free*0.8)
//	eligible <=> requiredBytes > 0 && requiredBytes <= availableBytes
//	engine limit  = requiredBytes (BinarySI)
//
// modelEstimateBytes is catalog memoryBytes (weights×2 + engine headroom already
// baked into discovery). shmBytes is the memory-backed /dev/shm emptyDir (counts
// against the pod cgroup). podMarginBytes is a small kubelet/cgroup margin.
// There is no hard-coded 24Gi product ceiling; an optional package maxMemory is
// only applied when explicitly configured.

const (
	podMarginBytes = 512 << 20 // 512Mi cgroup / runtime margin beyond model estimate
)

type engineOrchestrator interface {
	EnsureService(ctx context.Context) error
	ApplyEngine(ctx context.Context, spec enginePodSpec) error
	DeleteEngine(ctx context.Context) error
	WaitReady(ctx context.Context, timeout time.Duration) error
	EngineStatus(ctx context.Context) (engineStatus, error)
}

type enginePodSpec struct {
	ModelID     string
	Command     []string
	Args        []string
	MemoryLimit resource.Quantity
	MemoryReq   resource.Quantity
	CPULimit    resource.Quantity
	CPURequest  resource.Quantity
	GPURequest  bool
}

type engineStatus struct {
	Exists        bool
	Ready         bool
	Replicas      int32
	ReadyReplicas int32
	Phase         string
	Message       string
	OOMKilled     bool
}

// memoryPlan is the shared catalog↔Load resource decision.
type memoryPlan struct {
	ModelEstimateBytes uint64 `json:"modelEstimateBytes"`
	ShmBytes           uint64 `json:"shmBytes"`
	PodMarginBytes     uint64 `json:"podMarginBytes"`
	RequiredBytes      uint64 `json:"requiredBytes"`
	AvailableBytes     uint64 `json:"availableBytes"`
}

type k8sEngine struct {
	client      kubernetes.Interface
	namespace   string
	deployment  string
	service     string
	port        int32
	image       string
	instance    string
	modelsClaim string
	shmSize     resource.Quantity
	gpuEnabled  bool
	gpuRuntime  string
	gpuDevices  string
	gpuCaps     string
	pullSecrets []string
	engine      string // vllm|ollama
}

func newEngineOrchestratorFromEnv(engine string) (engineOrchestrator, error) {
	if os.Getenv("INFERENCE_ENGINE_ORCHESTRATOR") == "noop" {
		return noopEngine{}, nil
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
			return noopEngine{}, nil
		}
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(env("INFERENCE_ENGINE_PORT", "8001"))
	if port <= 0 {
		port = 8001
	}
	shm := resource.MustParse(env("INFERENCE_ENGINE_SHARED_MEMORY", "4Gi"))
	return &k8sEngine{
		client:      client,
		namespace:   env("INFERENCE_NAMESPACE", "inference"),
		deployment:  env("INFERENCE_ENGINE_DEPLOYMENT", "inference-engine"),
		service:     env("INFERENCE_ENGINE_SERVICE", "inference-engine"),
		port:        int32(port),
		image:       env("INFERENCE_RUNTIME_IMAGE", ""),
		instance:    env("INFERENCE_RELEASE_INSTANCE", "appliance-inference"),
		modelsClaim: env("INFERENCE_MODELS_CLAIM", "models"),
		shmSize:     shm,
		gpuEnabled:  strings.EqualFold(env("INFERENCE_GPU_ENABLED", "false"), "true"),
		gpuRuntime:  env("INFERENCE_GPU_RUNTIME_CLASS", "nvidia"),
		gpuDevices:  env("INFERENCE_GPU_VISIBLE_DEVICES", "all"),
		gpuCaps:     env("INFERENCE_GPU_DRIVER_CAPABILITIES", "all"),
		pullSecrets: splitCSV(env("INFERENCE_IMAGE_PULL_SECRETS", "")),
		engine:      engine,
	}, nil
}

type noopEngine struct{}

func (noopEngine) EnsureService(context.Context) error { return nil }
func (noopEngine) ApplyEngine(context.Context, enginePodSpec) error {
	return errors.New("engine orchestrator unavailable outside the cluster")
}
func (noopEngine) DeleteEngine(context.Context) error { return nil }
func (noopEngine) WaitReady(context.Context, time.Duration) error {
	return errors.New("engine orchestrator unavailable outside the cluster")
}
func (noopEngine) EngineStatus(context.Context) (engineStatus, error) {
	return engineStatus{}, nil
}

func splitCSV(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (e *k8sEngine) labels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     e.deployment,
		"app.kubernetes.io/instance": e.instance,
		"app.kubernetes.io/part-of":  "appliance-inference",
	}
}

func (e *k8sEngine) EnsureService(ctx context.Context) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      e.service,
			Namespace: e.namespace,
			Labels:    e.labels(),
		},
		Spec: corev1.ServiceSpec{
			Selector: e.labels(),
			Ports: []corev1.ServicePort{{
				Name:       "openai",
				Port:       e.port,
				TargetPort: intstr.FromInt(int(e.port)),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
	_, err := e.client.CoreV1().Services(e.namespace).Create(ctx, svc, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := e.client.CoreV1().Services(e.namespace).Get(ctx, e.service, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		existing.Spec.Selector = svc.Spec.Selector
		existing.Spec.Ports = svc.Spec.Ports
		_, err = e.client.CoreV1().Services(e.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	}
	return err
}

func (e *k8sEngine) DeleteEngine(ctx context.Context) error {
	err := e.client.AppsV1().Deployments(e.namespace).Delete(ctx, e.deployment, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		_, getErr := e.client.AppsV1().Deployments(e.namespace).Get(ctx, e.deployment, metav1.GetOptions{})
		if apierrors.IsNotFound(getErr) {
			return nil
		}
		if getErr != nil {
			return getErr
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for previous engine Deployment to delete")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (e *k8sEngine) ApplyEngine(ctx context.Context, spec enginePodSpec) error {
	if strings.TrimSpace(e.image) == "" {
		return errors.New("INFERENCE_RUNTIME_IMAGE is required")
	}
	replicas := int32(1)
	fsGroup := int64(20000)
	runAs := int64(10006)
	nonRoot := true
	readOnly := true
	allowPriv := false
	dropAll := []corev1.Capability{"ALL"}

	envVars := []corev1.EnvVar{
		{Name: "HOME", Value: "/home/runtime"},
		{Name: "USER", Value: "runtime"},
		{Name: "LOGNAME", Value: "runtime"},
		{Name: "HF_HOME", Value: "/models/.cache/huggingface"},
		{Name: "VLLM_CACHE_ROOT", Value: "/models/.cache/vllm"},
		{Name: "HF_HUB_OFFLINE", Value: "1"},
		{Name: "TRANSFORMERS_OFFLINE", Value: "1"},
		{Name: "HF_HUB_DISABLE_TELEMETRY", Value: "1"},
		{Name: "VLLM_NO_USAGE_STATS", Value: "1"},
		{Name: "DO_NOT_TRACK", Value: "1"},
		{Name: "TORCHINDUCTOR_CACHE_DIR", Value: "/home/runtime/.cache/torch/inductor"},
		{Name: "TRITON_CACHE_DIR", Value: "/home/runtime/.cache/triton"},
		{Name: "XDG_CACHE_HOME", Value: "/home/runtime/.cache"},
	}
	if !e.gpuEnabled {
		envVars = append(envVars,
			corev1.EnvVar{Name: "CUDA_VISIBLE_DEVICES", Value: "-1"},
			corev1.EnvVar{Name: "ROCR_VISIBLE_DEVICES", Value: "-1"},
		)
	} else {
		envVars = append(envVars,
			corev1.EnvVar{Name: "NVIDIA_VISIBLE_DEVICES", Value: e.gpuDevices},
			corev1.EnvVar{Name: "NVIDIA_DRIVER_CAPABILITIES", Value: e.gpuCaps},
		)
	}
	if e.engine == "ollama" {
		envVars = append(envVars,
			corev1.EnvVar{Name: "OLLAMA_HOST", Value: fmt.Sprintf("0.0.0.0:%d", e.port)},
			corev1.EnvVar{Name: "OLLAMA_MODELS", Value: "/models"},
		)
	} else {
		envVars = append(envVars,
			corev1.EnvVar{Name: "VLLM_CPU_KVCACHE_SPACE", Value: env("VLLM_CPU_KVCACHE_SPACE", "2")},
			corev1.EnvVar{Name: "VLLM_CPU_OMP_THREADS_BIND", Value: "auto"},
			corev1.EnvVar{Name: "VLLM_CPU_NUM_OF_RESERVED_CPU", Value: "1"},
		)
	}

	mounts := []corev1.VolumeMount{
		{Name: "models", MountPath: "/models"},
		{Name: "tmp", MountPath: "/tmp"},
		{Name: "runtime-home", MountPath: "/home/runtime"},
	}
	volumes := []corev1.Volume{
		{Name: "models", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: e.modelsClaim}}},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "runtime-home", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	if e.engine == "vllm" {
		mounts = append(mounts, corev1.VolumeMount{Name: "shared-memory", MountPath: "/dev/shm"})
		volumes = append(volumes, corev1.Volume{
			Name: "shared-memory",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium:    corev1.StorageMediumMemory,
				SizeLimit: &e.shmSize,
			}},
		})
	}

	limits := corev1.ResourceList{
		corev1.ResourceMemory: spec.MemoryLimit,
		corev1.ResourceCPU:    spec.CPULimit,
	}
	requests := corev1.ResourceList{
		corev1.ResourceMemory: spec.MemoryReq,
		corev1.ResourceCPU:    spec.CPURequest,
	}
	if spec.GPURequest {
		limits[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("1")
		requests[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("1")
	}

	container := corev1.Container{
		Name:            "inference-engine",
		Image:           e.image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         spec.Command,
		Args:            spec.Args,
		Env:             envVars,
		Ports: []corev1.ContainerPort{{
			Name:          "openai",
			ContainerPort: e.port,
			Protocol:      corev1.ProtocolTCP,
		}},
		Resources: corev1.ResourceRequirements{
			Limits:   limits,
			Requests: requests,
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &allowPriv,
			ReadOnlyRootFilesystem:   &readOnly,
			Capabilities:             &corev1.Capabilities{Drop: dropAll},
		},
		VolumeMounts: mounts,
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
				Path: healthPathForEngine(e.engine),
				Port: intstr.FromInt(int(e.port)),
			}},
			PeriodSeconds:    5,
			TimeoutSeconds:   2,
			FailureThreshold: 12,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
				Path: healthPathForEngine(e.engine),
				Port: intstr.FromInt(int(e.port)),
			}},
			PeriodSeconds:    15,
			TimeoutSeconds:   3,
			FailureThreshold: 5,
		},
		StartupProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{
				Path: healthPathForEngine(e.engine),
				Port: intstr.FromInt(int(e.port)),
			}},
			PeriodSeconds:    5,
			TimeoutSeconds:   3,
			FailureThreshold: 120,
		},
	}

	podSpec := corev1.PodSpec{
		AutomountServiceAccountToken: boolPtr(false),
		SecurityContext: &corev1.PodSecurityContext{
			RunAsNonRoot:        &nonRoot,
			RunAsUser:           &runAs,
			RunAsGroup:          &runAs,
			FSGroup:             &fsGroup,
			FSGroupChangePolicy: fsGroupPolicyPtr(corev1.FSGroupChangeOnRootMismatch),
			SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
		Containers: []corev1.Container{container},
		Volumes:    volumes,
	}
	if e.gpuEnabled && e.gpuRuntime != "" {
		podSpec.RuntimeClassName = &e.gpuRuntime
	}
	for _, name := range e.pullSecrets {
		podSpec.ImagePullSecrets = append(podSpec.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      e.deployment,
			Namespace: e.namespace,
			Labels:    e.labels(),
			Annotations: map[string]string{
				"appliance.inference/model-id": spec.ModelID,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: e.labels()},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: e.labels()},
				Spec:       podSpec,
			},
		},
	}

	_, err := e.client.AppsV1().Deployments(e.namespace).Create(ctx, deploy, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := e.client.AppsV1().Deployments(e.namespace).Get(ctx, e.deployment, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		deploy.ResourceVersion = existing.ResourceVersion
		_, err = e.client.AppsV1().Deployments(e.namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	}
	return err
}

func healthPathForEngine(engine string) string {
	if engine == "ollama" {
		return "/"
	}
	return "/health"
}

func boolPtr(v bool) *bool { return &v }

func fsGroupPolicyPtr(v corev1.PodFSGroupChangePolicy) *corev1.PodFSGroupChangePolicy {
	return &v
}

func (e *k8sEngine) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		status, err := e.EngineStatus(ctx)
		if err != nil {
			return err
		}
		if status.Ready {
			return nil
		}
		if status.OOMKilled {
			return fmt.Errorf("engine pod OOMKilled: %s", status.Message)
		}
		if time.Now().After(deadline) {
			if status.Message != "" {
				return fmt.Errorf("engine not ready: %s", status.Message)
			}
			return errors.New("engine not ready: timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (e *k8sEngine) EngineStatus(ctx context.Context) (engineStatus, error) {
	dep, err := e.client.AppsV1().Deployments(e.namespace).Get(ctx, e.deployment, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return engineStatus{}, nil
	}
	if err != nil {
		return engineStatus{}, err
	}
	st := engineStatus{
		Exists:        true,
		Replicas:      dep.Status.Replicas,
		ReadyReplicas: dep.Status.ReadyReplicas,
		Ready:         dep.Status.ReadyReplicas >= 1 && dep.Status.UnavailableReplicas == 0,
	}
	pods, err := e.client.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", e.deployment),
	})
	if err != nil {
		return st, nil
	}
	for _, pod := range pods.Items {
		st.Phase = string(pod.Status.Phase)
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != "inference-engine" {
				continue
			}
			if cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
				st.OOMKilled = true
				st.Message = "OOMKilled"
			}
			if cs.State.Waiting != nil {
				st.Message = cs.State.Waiting.Reason + ": " + cs.State.Waiting.Message
			}
			if cs.State.Terminated != nil {
				st.Message = cs.State.Terminated.Reason
				if cs.State.Terminated.Reason == "OOMKilled" {
					st.OOMKilled = true
				}
			}
		}
		for _, cond := range pod.Status.Conditions {
			if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse {
				st.Message = cond.Reason + ": " + cond.Message
			}
		}
	}
	return st, nil
}

func idleOllamaResources() enginePodSpec {
	return enginePodSpec{
		MemoryLimit: resource.MustParse("4Gi"),
		MemoryReq:   resource.MustParse("1Gi"),
		CPULimit:    resource.MustParse("2"),
		CPURequest:  resource.MustParse("100m"),
	}
}

func parseQuantity(value string, fallback string) resource.Quantity {
	q, err := resource.ParseQuantity(strings.TrimSpace(value))
	if err != nil {
		if fallback == "" {
			return resource.Quantity{}
		}
		return resource.MustParse(fallback)
	}
	return q
}

func configuredShmBytes(engine string) uint64 {
	if engine != "vllm" {
		return 0
	}
	shm := parseQuantity(env("INFERENCE_ENGINE_SHARED_MEMORY", "4Gi"), "4Gi")
	if shm.Value() <= 0 {
		return 0
	}
	return uint64(shm.Value())
}

// planModelMemory is the single function used by catalog eligibility and Load.
func planModelMemory(engine string, modelEstimateBytes, availableBytes uint64) (memoryPlan, error) {
	plan := memoryPlan{
		ModelEstimateBytes: modelEstimateBytes,
		ShmBytes:           configuredShmBytes(engine),
		PodMarginBytes:     podMarginBytes,
		AvailableBytes:     availableBytes,
	}
	if modelEstimateBytes == 0 {
		return plan, errors.New("model memory estimate is unavailable")
	}
	plan.RequiredBytes = modelEstimateBytes + plan.ShmBytes + plan.PodMarginBytes
	if availableBytes > 0 && plan.RequiredBytes > availableBytes {
		return plan, fmt.Errorf("model needs %d bytes (estimate %d + shm %d + margin %d); only %d bytes available",
			plan.RequiredBytes, modelEstimateBytes, plan.ShmBytes, plan.PodMarginBytes, availableBytes)
	}
	if max := packageMaxMemoryBytes(); max > 0 && plan.RequiredBytes > max {
		return plan, fmt.Errorf("model needs %d bytes; configured package max is %d bytes", plan.RequiredBytes, max)
	}
	return plan, nil
}

func engineResourceSpec(engine string, modelEstimateBytes, budgetMemory uint64) (enginePodSpec, error) {
	plan, err := planModelMemory(engine, modelEstimateBytes, budgetMemory)
	if err != nil {
		return enginePodSpec{}, err
	}
	cpuLimit := parseQuantity(env("INFERENCE_ENGINE_CPU_LIMIT", "4"), "4")
	cpuReq := parseQuantity(env("INFERENCE_ENGINE_CPU_REQUEST", "100m"), "100m")
	limit := *resource.NewQuantity(int64(plan.RequiredBytes), resource.BinarySI)
	req := *resource.NewQuantity(limit.Value()/2, resource.BinarySI)
	if req.Cmp(resource.MustParse("512Mi")) < 0 {
		req = resource.MustParse("512Mi")
	}
	return enginePodSpec{
		MemoryLimit: limit,
		MemoryReq:   req,
		CPULimit:    cpuLimit,
		CPURequest:  cpuReq,
		GPURequest:  strings.EqualFold(env("INFERENCE_GPU_ENABLED", "false"), "true"),
	}, nil
}

// packageMaxMemoryBytes returns an optional operator/package ceiling.
// Empty INFERENCE_ENGINE_MAX_MEMORY means "host budget only".
func packageMaxMemoryBytes() uint64 {
	raw := strings.TrimSpace(os.Getenv("INFERENCE_ENGINE_MAX_MEMORY"))
	if raw == "" {
		return 0
	}
	maxMem := parseQuantity(raw, "")
	if maxMem.Value() <= 0 {
		return 0
	}
	return uint64(maxMem.Value())
}

func persistEngineDesire(modelsDir, modelID string, spec enginePodSpec) error {
	dir := filepath.Join(modelsDir, ".zon")
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(map[string]any{
		"modelId":     modelID,
		"memoryLimit": spec.MemoryLimit.String(),
		"updatedAt":   time.Now().UTC(),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "engine-desire.json"), payload, 0o640)
}

func clearEngineDesire(modelsDir string) {
	_ = os.Remove(filepath.Join(modelsDir, ".zon", "engine-desire.json"))
}
