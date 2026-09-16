{{- define "appliance-inference.name" -}}
{{- /* K8s resource basename: inference-gateway (not the chart name). */ -}}
{{- .Values.nameOverride | default "inference-gateway" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-inference.fullname" -}}
{{- .Values.fullnameOverride | default "inference-gateway" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-inference.namespace" -}}
{{- default .Release.Namespace .Values.namespace.name -}}
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
- name: INFERENCE_MODE
  value: {{ .Values.runtime.mode | quote }}
- name: INFERENCE_SUPPORTED_MODES
  value: {{ join "," .Values.runtime.supportedModes | quote }}
- name: INFERENCE_LISTEN_ADDRESS
  value: "0.0.0.0:11434"
- name: INFERENCE_BACKEND_URL
  value: "http://127.0.0.1:8001"
- name: INFERENCE_MODELS_DIR
  value: "/models"
- name: INFERENCE_CONTROL_DIR
  value: "/control"
- name: HF_HOME
  value: "/models/.cache/huggingface"
- name: HUGGINGFACE_HUB_CACHE
  value: "/models/.cache/huggingface/hub"
- name: INFERENCE_GPU_ENABLED
  value: {{ .Values.gpu.enabled | quote }}
{{- if eq .Values.runtime.engine "vllm" }}
- name: VLLM_CPU_KVCACHE_SPACE
  value: {{ .Values.runtime.cpuKVCacheSpaceGiB | quote }}
{{- end }}
{{- end -}}

{{- define "appliance-inference.engineEnv" -}}
- name: HOME
  value: "/home/runtime"
- name: USER
  value: "runtime"
- name: LOGNAME
  value: "runtime"
{{- if not .Values.gpu.enabled }}
- name: CUDA_VISIBLE_DEVICES
  value: "-1"
- name: ROCR_VISIBLE_DEVICES
  value: "-1"
{{- end }}
{{- if eq .Values.runtime.engine "ollama" }}
- name: OLLAMA_HOST
  value: "127.0.0.1:8001"
- name: OLLAMA_MODELS
  value: "/models"
{{- else if eq .Values.runtime.engine "vllm" }}
- name: INFERENCE_CONTROL_DIR
  value: "/control"
- name: HF_HOME
  value: "/models/.cache/huggingface"
- name: VLLM_CACHE_ROOT
  value: "/models/.cache/vllm"
- name: HF_HUB_OFFLINE
  value: "1"
- name: TRANSFORMERS_OFFLINE
  value: "1"
- name: HF_HUB_DISABLE_TELEMETRY
  value: "1"
- name: VLLM_NO_USAGE_STATS
  value: "1"
- name: DO_NOT_TRACK
  value: "1"
- name: VLLM_CPU_KVCACHE_SPACE
  value: {{ .Values.runtime.cpuKVCacheSpaceGiB | quote }}
- name: VLLM_CPU_OMP_THREADS_BIND
  value: "auto"
- name: VLLM_CPU_NUM_OF_RESERVED_CPU
  value: "1"
- name: TORCHINDUCTOR_CACHE_DIR
  value: "/home/runtime/.cache/torch/inductor"
- name: TRITON_CACHE_DIR
  value: "/home/runtime/.cache/triton"
- name: XDG_CACHE_HOME
  value: "/home/runtime/.cache"
{{- else -}}
{{- fail "unsupported inference runtime engine/modes; install a compatible signed runtime package" -}}
{{- end -}}
{{- end -}}
