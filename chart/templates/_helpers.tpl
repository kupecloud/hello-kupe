{{- define "hello-kupe.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "hello-kupe.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s" (include "hello-kupe.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "hello-kupe.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" -}}
{{- end -}}

{{- define "hello-kupe.labels" -}}
helm.sh/chart: {{ include "hello-kupe.chart" . }}
app.kubernetes.io/name: {{ include "hello-kupe.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "hello-kupe.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hello-kupe.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "hello-kupe.hostname" -}}
{{- if .Values.httpRoute.hostname -}}
{{- .Values.httpRoute.hostname -}}
{{- else -}}
{{- $tenant := required "A value for tenant is required when httpRoute.hostname is not set" .Values.tenant -}}
{{- printf "%s.%s.%s" .Values.httpRoute.hostPrefix $tenant .Values.domain -}}
{{- end -}}
{{- end -}}
