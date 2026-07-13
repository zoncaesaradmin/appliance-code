{{/*
Expand the name of the chart.
*/}}
{{- define "appliance-control-plane.name" -}}
{{- .Values.nameOverride | default .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a fully qualified app name.
*/}}
{{- define "appliance-control-plane.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := .Values.nameOverride | default .Chart.Name -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Namespace this release targets.
*/}}
{{- define "appliance-control-plane.namespace" -}}
{{- .Values.namespace.name | default .Release.Namespace -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "appliance-control-plane.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "appliance-control-plane.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Common labels for the UI component.
*/}}
{{- define "appliance-control-plane.uiLabels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "appliance-control-plane.uiSelectorLabels" . }}
app.kubernetes.io/component: ui
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "appliance-control-plane.selectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-control-plane.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Selector labels for the UI pod.
*/}}
{{- define "appliance-control-plane.uiSelectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-control-plane.name" . }}-ui
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
ServiceAccount name.
*/}}
{{- define "appliance-control-plane.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- .Values.serviceAccount.name | default (include "appliance-control-plane.fullname" .) -}}
{{- else -}}
{{- .Values.serviceAccount.name | default "default" -}}
{{- end -}}
{{- end -}}

{{/*
Image reference, preferring an explicit digest pin over a tag.
*/}}
{{- define "appliance-control-plane.image" -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" .Values.image.repository .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}
{{- end -}}

{{/*
UI image reference, preferring an explicit digest pin over a tag.
*/}}
{{- define "appliance-control-plane.uiImage" -}}
{{- if .Values.ui.image.digest -}}
{{- printf "%s@%s" .Values.ui.image.repository .Values.ui.image.digest -}}
{{- else -}}
{{- printf "%s:%s" .Values.ui.image.repository (.Values.ui.image.tag | default .Chart.AppVersion) -}}
{{- end -}}
{{- end -}}

{{/*
UI service name.
*/}}
{{- define "appliance-control-plane.uiServiceName" -}}
{{- printf "%s-ui" (include "appliance-control-plane.fullname" .) -}}
{{- end -}}

{{/*
Default UI -> control-plane public base URL. Allows an explicit override, but
keeps the common in-chart case aligned with the rendered Service name.
*/}}
{{- define "appliance-control-plane.uiControlPlaneBaseURL" -}}
{{- if .Values.ui.config.controlPlaneBaseURL -}}
{{- .Values.ui.config.controlPlaneBaseURL -}}
{{- else -}}
{{- printf "http://%s:%d" (include "appliance-control-plane.fullname" .) (.Values.service.publicPort | int) -}}
{{- end -}}
{{- end -}}

{{/*
Default UI -> control-plane internal base URL. Allows an explicit override, but
keeps the common in-chart case aligned with the rendered internal Service name.
*/}}
{{- define "appliance-control-plane.uiControlPlaneInternalBaseURL" -}}
{{- if .Values.ui.config.controlPlaneInternalBaseURL -}}
{{- .Values.ui.config.controlPlaneInternalBaseURL -}}
{{- else -}}
{{- printf "http://%s-internal:%d" (include "appliance-control-plane.fullname" .) (.Values.service.internalPort | int) -}}
{{- end -}}
{{- end -}}

{{/*
ForwardAuth middleware name.
*/}}
{{- define "appliance-control-plane.forwardAuthMiddlewareName" -}}
{{- printf "%s-forward-auth" (include "appliance-control-plane.fullname" .) -}}
{{- end -}}
