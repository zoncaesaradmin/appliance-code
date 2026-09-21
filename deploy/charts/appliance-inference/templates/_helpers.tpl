{{- define "appliance-inference.name" -}}
{{- /* K8s resource basename: inference-gateway (not the chart name). */ -}}
{{- .Values.nameOverride | default "inference-gateway" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-inference.fullname" -}}
{{- .Values.fullnameOverride | default "inference-gateway" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-inference.engineName" -}}
{{- .Values.engine.serviceName | default "inference-engine" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-inference.namespace" -}}
{{- default .Release.Namespace .Values.namespace.name -}}
{{- end -}}

{{- define "appliance-inference.serviceAccountName" -}}
{{- printf "%s-manager" (include "appliance-inference.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-inference.labels" -}}
app.kubernetes.io/name: {{ include "appliance-inference.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "appliance-inference.selectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-inference.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "appliance-inference.engineSelectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-inference.engineName" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "appliance-inference.image" -}}
{{- if .Values.image.digest -}}
{{ printf "%s@%s" .Values.image.repository .Values.image.digest }}
{{- else -}}
{{ printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end -}}
{{- end -}}

{{- define "appliance-inference.managerImage" -}}
{{- if .Values.managerImage.digest -}}
{{ printf "%s@%s" .Values.managerImage.repository .Values.managerImage.digest }}
{{- else -}}
{{ printf "%s:%s" .Values.managerImage.repository (default .Chart.AppVersion .Values.managerImage.tag) }}
{{- end -}}
{{- end -}}

{{- define "appliance-inference.modelsVolumeName" -}}
{{- printf "%s-models" (include "appliance-inference.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-inference.managerEnv" -}}
- name: HOME
  value: "/home/runtime"
- name: INFERENCE_ENGINE
  value: {{ .Values.runtime.engine | quote }}
- name: INFERENCE_LISTEN_ADDRESS
  value: "0.0.0.0:11434"
- name: INFERENCE_BACKEND_URL
  value: {{ printf "http://%s.%s.svc.cluster.local:%v" (include "appliance-inference.engineName" .) (include "appliance-inference.namespace" .) .Values.engine.servicePort | quote }}
- name: INFERENCE_MODELS_DIR
  value: "/models"
- name: INFERENCE_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
- name: INFERENCE_ENGINE_DEPLOYMENT
  value: {{ include "appliance-inference.engineName" . | quote }}
- name: INFERENCE_ENGINE_SERVICE
  value: {{ include "appliance-inference.engineName" . | quote }}
- name: INFERENCE_ENGINE_PORT
  value: {{ .Values.engine.servicePort | quote }}
- name: INFERENCE_RUNTIME_IMAGE
  value: {{ include "appliance-inference.image" . | quote }}
- name: INFERENCE_RUNTIME_VERSION
  value: {{ .Chart.AppVersion | trimPrefix "v" | quote }}
{{- if .Values.engine.maxMemory }}
- name: INFERENCE_ENGINE_MAX_MEMORY
  value: {{ .Values.engine.maxMemory | quote }}
{{- end }}
{{- if .Values.engine.cpuLimit }}
- name: INFERENCE_ENGINE_CPU_LIMIT
  value: {{ .Values.engine.cpuLimit | quote }}
{{- end }}
{{- if .Values.engine.cpuRequest }}
- name: INFERENCE_ENGINE_CPU_REQUEST
  value: {{ .Values.engine.cpuRequest | quote }}
{{- end }}
- name: INFERENCE_ENGINE_SHARED_MEMORY
  value: {{ .Values.engine.sharedMemorySize | default .Values.runtime.sharedMemorySize | quote }}
- name: INFERENCE_RELEASE_INSTANCE
  value: {{ .Release.Name | quote }}
- name: INFERENCE_MODELS_CLAIM
  value: {{ .Values.persistence.claimName | quote }}
- name: HF_HOME
  value: "/models/.cache/huggingface"
- name: HUGGINGFACE_HUB_CACHE
  value: "/models/.cache/huggingface/hub"
- name: INFERENCE_GPU_ENABLED
  value: {{ .Values.gpu.enabled | quote }}
{{- if .Values.gpu.enabled }}
- name: INFERENCE_GPU_RUNTIME_CLASS
  value: {{ .Values.gpu.runtimeClassName | quote }}
- name: INFERENCE_GPU_VISIBLE_DEVICES
  value: {{ .Values.gpu.visibleDevices | quote }}
- name: INFERENCE_GPU_DRIVER_CAPABILITIES
  value: {{ .Values.gpu.driverCapabilities | quote }}
{{- end }}
{{- with .Values.imagePullSecrets }}
- name: INFERENCE_IMAGE_PULL_SECRETS
  value: "{{ range $i, $s := . }}{{ if $i }},{{ end }}{{ $s.name }}{{ end }}"
{{- end }}
{{- if eq .Values.runtime.engine "vllm" }}
- name: VLLM_CPU_KVCACHE_SPACE
  value: {{ .Values.runtime.cpuKVCacheSpaceGiB | quote }}
- name: INFERENCE_ARCH_FILE
  value: "/models/.zon/vllm-architectures.json"
{{- end }}
{{- end -}}
