{{- define "appliance-control-plane.imagePullSecrets" -}}
{{- with .Values.imagePullSecrets }}
imagePullSecrets:
{{- toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

{{/*
Expand the name of the chart.
*/}}
{{- define "appliance-control-plane.name" -}}
{{- /* K8s resource basename: controlplane (not the chart/image name appliance-control-plane). */ -}}
{{- .Values.nameOverride | default "controlplane" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a fully qualified app name.
*/}}
{{- define "appliance-control-plane.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "appliance-control-plane.name" . -}}
{{- end -}}
{{- end -}}

{{/*
Namespace for controlplane Deployment/Service/PVC/keys (Helm release namespace).
*/}}
{{- define "appliance-control-plane.namespace" -}}
{{- .Values.namespace.name | default .Release.Namespace -}}
{{- end -}}

{{/*
Namespace for operator-facing apps co-packaged with the control-plane chart:
ui-server, host-agent, automation-runtime. Defaults to co-locating with
controlplane when appsNamespace.name is empty (single-namespace tests);
production injects ace-apps via zonctl.
*/}}
{{- define "appliance-control-plane.appsNamespace" -}}
{{- $apps := "" -}}
{{- if .Values.appsNamespace -}}
{{- $apps = .Values.appsNamespace.name | default "" -}}
{{- end -}}
{{- if $apps -}}
{{- $apps -}}
{{- else -}}
{{- include "appliance-control-plane.namespace" . -}}
{{- end -}}
{{- end -}}

{{/*
Whether apps are isolated in a namespace other than the controlplane.
*/}}
{{- define "appliance-control-plane.appsNamespaced" -}}
{{- if ne (include "appliance-control-plane.appsNamespace" .) (include "appliance-control-plane.namespace" .) -}}
true
{{- end -}}
{{- end -}}

{{/*
Permanent namespace for user-managed application workloads.
*/}}
{{- define "appliance-control-plane.applicationNamespace" -}}
{{- .Values.applicationNamespace.name | default "apps" -}}
{{- end -}}

{{/*
Cluster-local DNS name for a Service (optionally cross-namespace).
Args: list name namespace
*/}}
{{- define "appliance-control-plane.serviceDNS" -}}
{{- $name := index . 0 -}}
{{- $ns := index . 1 -}}
{{- printf "%s.%s.svc.cluster.local" $name $ns -}}
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
Common labels for the host-agent component.
*/}}
{{- define "appliance-control-plane.hostAgentLabels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "appliance-control-plane.hostAgentSelectorLabels" . }}
app.kubernetes.io/component: host-agent
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Common labels for the automation runtime component.
*/}}
{{- define "appliance-control-plane.automationRuntimeLabels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "appliance-control-plane.automationRuntimeSelectorLabels" . }}
app.kubernetes.io/component: automation-runtime
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
app.kubernetes.io/name: {{ include "appliance-control-plane.uiServiceName" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Selector labels for the host-agent pod.
*/}}
{{- define "appliance-control-plane.hostAgentSelectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-control-plane.hostAgentName" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Selector labels for the automation-runtime pod.
*/}}
{{- define "appliance-control-plane.automationRuntimeSelectorLabels" -}}
app.kubernetes.io/name: {{ include "appliance-control-plane.automationRuntimeName" . }}
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
Whether the control plane should get API access for workflow submission.
*/}}
{{- define "appliance-control-plane.workflowsEnabled" -}}
{{- if or (eq .Values.config.applianceProfile "builder") (eq .Values.config.applianceProfile "builder-landns") (eq .Values.config.applianceProfile "builder-storage-landns") -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{/*
Whether the control plane manages LAN DNS zone ConfigMap sync.
*/}}
{{- define "appliance-control-plane.dnsAdminEnabled" -}}
{{- if or (eq .Values.config.applianceProfile "landns") (eq .Values.config.applianceProfile "storage-landns") (eq .Values.config.applianceProfile "builder-landns") (eq .Values.config.applianceProfile "builder-storage-landns") -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{/*
Whether the control-plane ServiceAccount token must be mounted.
*/}}
{{- define "appliance-control-plane.serviceAccountTokenRequired" -}}
{{- if or (eq (include "appliance-control-plane.workflowsEnabled" .) "true") (eq (include "appliance-control-plane.dnsAdminEnabled" .) "true") -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{/*
Fixed namespace for appliance-owned workflows in v1.
*/}}
{{- define "appliance-control-plane.workflowNamespace" -}}
appliance-builds
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
UI Deployment/Service name. Independent of controlplane fullname so pods are
ui-server-* rather than controlplane-ui-*.
*/}}
{{- define "appliance-control-plane.uiServiceName" -}}
{{- .Values.ui.nameOverride | default "ui-server" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Default UI -> control-plane public base URL (cluster DNS, works cross-namespace).
*/}}
{{- define "appliance-control-plane.uiControlPlaneBaseURL" -}}
{{- if .Values.ui.config.controlPlaneBaseURL -}}
{{- .Values.ui.config.controlPlaneBaseURL -}}
{{- else -}}
{{- printf "http://%s:%d" (include "appliance-control-plane.serviceDNS" (list (include "appliance-control-plane.fullname" .) (include "appliance-control-plane.namespace" .))) (.Values.service.publicPort | int) -}}
{{- end -}}
{{- end -}}

{{/*
Default UI -> control-plane internal base URL (cluster DNS, works cross-namespace).
*/}}
{{- define "appliance-control-plane.uiControlPlaneInternalBaseURL" -}}
{{- if .Values.ui.config.controlPlaneInternalBaseURL -}}
{{- .Values.ui.config.controlPlaneInternalBaseURL -}}
{{- else -}}
{{- printf "http://%s:%d" (include "appliance-control-plane.serviceDNS" (list (printf "%s-internal" (include "appliance-control-plane.fullname" .)) (include "appliance-control-plane.namespace" .))) (.Values.service.internalPort | int) -}}
{{- end -}}
{{- end -}}

{{/*
controlplane -> automation-runtime base URL.
*/}}
{{- define "appliance-control-plane.automationRuntimeBaseURL" -}}
{{- printf "http://%s:%d" (include "appliance-control-plane.serviceDNS" (list (include "appliance-control-plane.automationRuntimeName" .) (include "appliance-control-plane.appsNamespace" .))) (.Values.automationRuntime.service.port | int) -}}
{{- end -}}

{{/*
Host agent Deployment/Service name. Independent of controlplane fullname so
pods are host-agent-* rather than controlplane-host-agent-*.
*/}}
{{- define "appliance-control-plane.hostAgentName" -}}
{{- .Values.hostAgent.nameOverride | default "host-agent" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Automation Runtime Deployment/Service name.
*/}}
{{- define "appliance-control-plane.automationRuntimeName" -}}
automation-runtime
{{- end -}}

{{/*
Automation Runtime PVC name (own SQLite store; never share the control-plane PVC).
*/}}
{{- define "appliance-control-plane.automationRuntimeClaimName" -}}
{{- printf "%s-data" (include "appliance-control-plane.automationRuntimeName" .) -}}
{{- end -}}

{{/*
Workspace PV name for the fixed host-path builder workspace storage.
*/}}
{{- define "appliance-control-plane.workspaceVolumeName" -}}
{{- printf "%s-workspaces" (include "appliance-control-plane.fullname" .) -}}
{{- end -}}

{{/*
Workspace PVC name for builder workflow pods.
*/}}
{{- define "appliance-control-plane.workspaceClaimName" -}}
{{- printf "%s-workspaces" (include "appliance-control-plane.fullname" .) -}}
{{- end -}}

{{/*
ForwardAuth middleware name.
*/}}
{{- define "appliance-control-plane.forwardAuthMiddlewareName" -}}
{{- printf "%s-forward-auth" (include "appliance-control-plane.fullname" .) -}}
{{- end -}}
