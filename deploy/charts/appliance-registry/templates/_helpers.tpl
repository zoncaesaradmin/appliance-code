{{- define "appliance-registry.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-registry.fullname" -}}
{{- default "appliance-registry" .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-registry.deploymentName" -}}
{{- default "artifact-registry" .Values.deploymentNameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-registry.namespace" -}}
{{- default .Release.Namespace .Values.namespace.name -}}
{{- end -}}

{{- define "appliance-registry.labels" -}}
app.kubernetes.io/name: {{ include "appliance-registry.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "appliance-registry.selectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-registry.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "appliance-registry.image" -}}
{{- if .Values.image.digest -}}
{{ printf "%s@%s" .Values.image.repository .Values.image.digest }}
{{- else -}}
{{ printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end -}}
{{- end -}}

{{- define "appliance-registry.fileserverFullname" -}}
fileserver
{{- end -}}

{{- define "appliance-registry.fileserverLabels" -}}
app.kubernetes.io/name: {{ include "appliance-registry.fileserverFullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: fileserver
{{- end -}}

{{- define "appliance-registry.fileserverSelectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-registry.fileserverFullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: fileserver
{{- end -}}

{{- define "appliance-registry.fileserverImage" -}}
{{- if .Values.fileserver.image.digest -}}
{{ printf "%s@%s" .Values.fileserver.image.repository .Values.fileserver.image.digest }}
{{- else -}}
{{ printf "%s:%s" .Values.fileserver.image.repository (default "bundled" .Values.fileserver.image.tag) }}
{{- end -}}
{{- end -}}
