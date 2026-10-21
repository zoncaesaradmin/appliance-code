{{- define "appliance-message-broker.name" -}}
{{- default "message-broker" .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "appliance-message-broker.fullname" -}}
{{- default "message-broker" .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "appliance-message-broker.labels" -}}
app.kubernetes.io/name: {{ include "appliance-message-broker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
