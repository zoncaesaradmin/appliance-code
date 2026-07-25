{{- define "appliance-dns.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-dns.fullname" -}}
{{- default "appliance-dns" .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-dns.namespace" -}}
{{- default .Release.Namespace .Values.namespace.name -}}
{{- end -}}

{{- define "appliance-dns.labels" -}}
app.kubernetes.io/name: {{ include "appliance-dns.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "appliance-dns.selectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-dns.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "appliance-dns.image" -}}
{{- if .Values.image.digest -}}
{{ printf "%s@%s" .Values.image.repository .Values.image.digest }}
{{- else -}}
{{ printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end -}}
{{- end -}}

{{- define "appliance-dns.fqdn" -}}
{{- printf "%s.%s" .Values.localZone.hostname .Values.localZone.name -}}
{{- end -}}
