{{/*
Expand the name of the release.
*/}}
{{- define "zabbix-easy.name" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "zabbix-easy.labels" -}}
app: {{ .Release.Name }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Internal PostgreSQL service hostname.
Format: <release-name>-postgres
*/}}
{{- define "zabbix-easy.postgresHost" -}}
{{- printf "%s-postgres" .Release.Name }}
{{- end }}

{{/*
Secret name for database credentials.
Format: <release-name>-db
*/}}
{{- define "zabbix-easy.dbSecret" -}}
{{- printf "%s-db" .Release.Name }}
{{- end }}

{{/*
Application environment variables.
Iterates .Values.env (non-sensitive) and appends DB credentials from Secret
and DB_HOST derived from the postgres service name when postgres.enabled=true.
*/}}
{{- define "zabbix-easy.env" }}
env:
  {{- range $key, $val := .Values.env }}
  - name: {{ $key }}
    value: {{ $val | quote }}
  {{- end }}
  {{- if .Values.postgres.enabled }}
  - name: DB_HOST
    value: {{ include "zabbix-easy.postgresHost" . | quote }}
  - name: DB_USER
    valueFrom:
      secretKeyRef:
        name: {{ include "zabbix-easy.dbSecret" . }}
        key: DB_USER
        optional: false
  - name: DB_PASSWORD
    valueFrom:
      secretKeyRef:
        name: {{ include "zabbix-easy.dbSecret" . }}
        key: DB_PASSWORD
        optional: false
  {{- end }}
{{- end }}
