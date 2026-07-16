{{- define "argo-workflows.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "argo-workflows.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "argo-workflows.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "argo-workflows.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "argo-workflows.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "argo-workflows.selectorLabels" -}}
app.kubernetes.io/name: {{ include "argo-workflows.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "argo-workflows.workflowsNamespace" -}}
{{- .Values.namespace.workflows -}}
{{- end -}}

{{- define "argo-workflows.buildsNamespace" -}}
{{- .Values.namespace.builds -}}
{{- end -}}

{{- define "argo-workflows.managedNamespace" -}}
{{- if .Values.controller.managedNamespace -}}
{{- .Values.controller.managedNamespace -}}
{{- else -}}
{{- include "argo-workflows.buildsNamespace" . -}}
{{- end -}}
{{- end -}}

{{- define "argo-workflows.controllerServiceAccountName" -}}
{{- if .Values.serviceAccount.controller.name -}}
{{- .Values.serviceAccount.controller.name -}}
{{- else -}}
{{- printf "%s-controller" (include "argo-workflows.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "argo-workflows.executorServiceAccountName" -}}
{{- if .Values.serviceAccount.executor.name -}}
{{- .Values.serviceAccount.executor.name -}}
{{- else -}}
{{- printf "%s-executor" (include "argo-workflows.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "argo-workflows.configMapName" -}}
{{- printf "%s-config" (include "argo-workflows.fullname" .) -}}
{{- end -}}

{{- define "argo-workflows.image" -}}
{{- $image := .image -}}
{{- if $image.digest -}}
{{- printf "%s@%s" $image.repository $image.digest -}}
{{- else -}}
{{- printf "%s:%s" $image.repository ($image.tag | default $.Chart.AppVersion) -}}
{{- end -}}
{{- end -}}
