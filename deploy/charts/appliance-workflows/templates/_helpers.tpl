{{- define "appliance-workflows.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-workflows.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "appliance-workflows.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "appliance-workflows.deploymentName" -}}
{{- default "workflow-controller" .Values.deploymentNameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "appliance-workflows.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "appliance-workflows.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "appliance-workflows.selectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-workflows.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "appliance-workflows.workflowsNamespace" -}}
{{- .Values.namespace.workflows -}}
{{- end -}}

{{- define "appliance-workflows.buildsNamespace" -}}
{{- .Values.namespace.builds -}}
{{- end -}}

{{- define "appliance-workflows.managedNamespace" -}}
{{- if .Values.controller.managedNamespace -}}
{{- .Values.controller.managedNamespace -}}
{{- else -}}
{{- include "appliance-workflows.buildsNamespace" . -}}
{{- end -}}
{{- end -}}

{{- define "appliance-workflows.controllerServiceAccountName" -}}
{{- if .Values.serviceAccount.controller.name -}}
{{- .Values.serviceAccount.controller.name -}}
{{- else -}}
{{- printf "%s-controller" (include "appliance-workflows.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "appliance-workflows.executorServiceAccountName" -}}
{{- if .Values.serviceAccount.executor.name -}}
{{- .Values.serviceAccount.executor.name -}}
{{- else -}}
{{- printf "%s-executor" (include "appliance-workflows.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "appliance-workflows.configMapName" -}}
{{- printf "%s-config" (include "appliance-workflows.fullname" .) -}}
{{- end -}}

{{- define "appliance-workflows.image" -}}
{{- $image := .image -}}
{{- if $image.digest -}}
{{- printf "%s@%s" $image.repository $image.digest -}}
{{- else -}}
{{- printf "%s:%s" $image.repository ($image.tag | default $.Chart.AppVersion) -}}
{{- end -}}
{{- end -}}
