{{- define "appliance-video.name" -}}
{{- /* K8s resource basename: video-gateway (not the chart name). */ -}}
{{- .Values.nameOverride | default "video-gateway" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-video.fullname" -}}
{{- .Values.fullnameOverride | default "video-gateway" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-video.namespace" -}}
{{- default .Release.Namespace .Values.namespace.name -}}
{{- end -}}

{{- define "appliance-video.labels" -}}
app.kubernetes.io/name: {{ include "appliance-video.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "appliance-video.selectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-video.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "appliance-video.image" -}}
{{- if .Values.image.digest -}}
{{ printf "%s@%s" .Values.image.repository .Values.image.digest }}
{{- else -}}
{{ printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end -}}
{{- end -}}

{{- define "appliance-video.libraryVolumeName" -}}
{{- printf "%s-library" (include "appliance-video.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
