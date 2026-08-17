{{- define "sentinelops.name" -}}sentinelops{{- end }}
{{- define "sentinelops.fullname" -}}{{ .Release.Name }}-sentinelops{{- end }}
{{- define "sentinelops.labels" -}}
app.kubernetes.io/name: {{ include "sentinelops.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}
{{- define "sentinelops.serviceAccountName" -}}{{ default (include "sentinelops.fullname" .) .Values.serviceAccount.name }}{{- end }}

