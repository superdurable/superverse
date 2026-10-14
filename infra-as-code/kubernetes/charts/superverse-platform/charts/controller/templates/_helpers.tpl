{{/*
Expand the name of the chart.
*/}}
{{- define "agent-sandbox.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
The namespace to deploy into.
*/}}
{{- define "agent-sandbox.namespace" -}}
{{- default .Release.Namespace .Values.namespace.name }}
{{- end }}

{{/*
The controller Service name.
*/}}
{{- define "agent-sandbox.serviceName" -}}
{{- default "agent-sandbox-controller" .Values.service.name }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "agent-sandbox.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" | quote }}
app.kubernetes.io/name: {{ include "agent-sandbox.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Values.image.tag }}
app.kubernetes.io/version: {{ first (splitList "@" .Values.image.tag) | trunc 63 | trimSuffix "-" | quote }}
{{- end }}
{{- end }}

{{/*
Selector labels used by Deployment and Service.
*/}}
{{- define "agent-sandbox.selectorLabels" -}}
app: agent-sandbox-controller
{{- end }}

{{/*
The controller image reference.
*/}}
{{- define "agent-sandbox.image" -}}
{{- $tag := required "image.tag is required" .Values.image.tag }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}
