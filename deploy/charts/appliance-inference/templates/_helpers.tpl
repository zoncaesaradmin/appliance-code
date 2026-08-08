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

{{- define "appliance-inference.modelsVolumeName" -}}
{{- printf "%s-models" (include "appliance-inference.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
