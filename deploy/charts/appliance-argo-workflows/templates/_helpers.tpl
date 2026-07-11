{{- define "appliance-argo-workflows.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-argo-workflows.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "appliance-argo-workflows.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "appliance-argo-workflows.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "appliance-argo-workflows.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "appliance-argo-workflows.selectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-argo-workflows.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "appliance-argo-workflows.workflowsNamespace" -}}
{{- .Values.namespace.workflows -}}
{{- end -}}

{{- define "appliance-argo-workflows.buildsNamespace" -}}
{{- .Values.namespace.builds -}}
{{- end -}}

{{- define "appliance-argo-workflows.managedNamespace" -}}
{{- if .Values.controller.managedNamespace -}}
{{- .Values.controller.managedNamespace -}}
{{- else -}}
{{- include "appliance-argo-workflows.buildsNamespace" . -}}
{{- end -}}
{{- end -}}

{{- define "appliance-argo-workflows.controllerServiceAccountName" -}}
{{- if .Values.serviceAccount.controller.name -}}
{{- .Values.serviceAccount.controller.name -}}
{{- else -}}
{{- printf "%s-controller" (include "appliance-argo-workflows.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "appliance-argo-workflows.executorServiceAccountName" -}}
{{- if .Values.serviceAccount.executor.name -}}
{{- .Values.serviceAccount.executor.name -}}
{{- else -}}
{{- printf "%s-executor" (include "appliance-argo-workflows.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "appliance-argo-workflows.configMapName" -}}
{{- printf "%s-config" (include "appliance-argo-workflows.fullname" .) -}}
{{- end -}}

{{- define "appliance-argo-workflows.image" -}}
{{- $image := .image -}}
{{- if $image.digest -}}
{{- printf "%s@%s" $image.repository $image.digest -}}
{{- else -}}
{{- printf "%s:%s" $image.repository ($image.tag | default $.Chart.AppVersion) -}}
{{- end -}}
{{- end -}}
