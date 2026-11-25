{{- define "todo.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" -}}
{{- end -}}

{{/* Labels for one component: pass (dict "root" $ "component" "api"). */}}
{{- define "todo.labels" -}}
helm.sh/chart: {{ include "todo.chart" .root }}
{{ include "todo.selectorLabels" . }}
app.kubernetes.io/version: {{ .root.Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
{{- end -}}

{{- define "todo.selectorLabels" -}}
app.kubernetes.io/name: todo
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "todo.image" -}}
{{- $repo := required (printf "%s.image.repository is required" .name) .image.repository -}}
{{- $tag := required (printf "%s.image.tag is required" .name) .image.tag -}}
{{- printf "%s:%v" $repo $tag -}}
{{- end -}}

{{- define "todo.hostname" -}}
{{- if .Values.httpRoute.hostname -}}
{{- .Values.httpRoute.hostname -}}
{{- else -}}
{{- $tenant := required "A value for tenant is required when httpRoute.hostname is not set" .Values.tenant -}}
{{- $cluster := required "A value for cluster is required when httpRoute.hostname is not set" .Values.cluster -}}
{{- printf "%s.%s.%s.clusters.%s" .Values.httpRoute.hostPrefix $cluster $tenant .Values.domain -}}
{{- end -}}
{{- end -}}

{{/*
Pod-level security shared by every workload. The images run as distroless
nonroot (UID 65532); fsGroup makes the mounted binding Secret readable to
that user while defaultMode keeps it unreadable to anyone else.
*/}}
{{- define "todo.podSecurityContext" -}}
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
fsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "todo.containerSecurityContext" -}}
allowPrivilegeEscalation: false
capabilities:
  drop:
    - ALL
readOnlyRootFilesystem: true
runAsNonRoot: true
{{- end -}}

{{/*
Binding Secret plumbing. Pass (dict "root" $ "secretName" "...").

The Secret is mounted as files at <bindingRoot>/<secretName>
(servicebinding.io). Files rather than environment variables, because:
  - libpq and pgx verify the server against `sslrootcert`, which must be a
    path to ca.crt on disk, not the certificate itself;
  - the Secret's `uri` key names that mount path for sslrootcert, so an
    application that reads `uri` works unchanged;
  - the kubelet updates mounted files when the Secret changes, and the API
    re-reads `password` for every new connection, so a rotated password is
    used without a restart.
*/}}
{{- define "todo.bindingVolume" -}}
- name: database-binding
  secret:
    secretName: {{ .secretName }}
    defaultMode: 0440
{{- end -}}

{{- define "todo.bindingMount" -}}
- name: database-binding
  mountPath: {{ printf "%s/%s" .root.Values.bindingRoot .secretName }}
  readOnly: true
{{- end -}}

{{- define "todo.bindingEnv" -}}
- name: SERVICE_BINDING_ROOT
  value: {{ .root.Values.bindingRoot | quote }}
- name: DATABASE_BINDING
  value: {{ .secretName | quote }}
{{- end -}}
