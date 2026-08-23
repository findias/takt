{{/*
Имена и общие метки. Вынесены сюда, чтобы имя ресурса нельзя было
случайно написать по-разному в двух шаблонах: селектор, разошедшийся
с метками пода, даёт службу без адресов и час поисков.
*/}}

{{- define "board.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "board.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if hasPrefix .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "board.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "board.labels" -}}
app.kubernetes.io/name: {{ include "board.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{/*
Селектор отделён от меток: в него не должна попадать версия. Иначе
при обновлении appVersion селектор Deployment изменился бы, а он
неизменяем — выкладка упала бы на ровном месте.
*/}}
{{- define "board.selectorLabels" -}}
app.kubernetes.io/name: {{ include "board.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "board.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "board.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Ссылка на образ. Digest перевешивает тег: в закрытом контуре тег
в зеркале переписывается, а digest — нет.
*/}}
{{- define "board.image" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ default .Chart.AppVersion .Values.image.tag }}
{{- end -}}
{{- end -}}

{{/*
Секрет со строкой подключения: либо принесённый заказчиком, либо
созданный чартом из значения. Второй способ оставлен для стенда;
на бою пароль базы не должен попадать в values.
*/}}
{{- define "board.secretName" -}}
{{- if .Values.postgresql.enabled -}}
{{ printf "%s-db" (include "board.fullname" .) }}
{{- else -}}
{{- default (printf "%s-db" (include "board.fullname" .)) .Values.database.existingSecret -}}
{{- end -}}
{{- end -}}

{{/*
Имя базы в кластере. Отдельным именем, а не «fullname плюс суффикс»
в каждом шаблоне: селектор, разошедшийся с метками, даёт службу
без адресов.
*/}}
{{- define "board.postgresName" -}}
{{ printf "%s-postgres" (include "board.fullname" .) | trunc 63 | trimSuffix "-" }}
{{- end -}}

{{/*
Строка подключения к базе в кластере. Собирается здесь, а не пишется
руками в values: адрес службы и имя роли задаются в двух местах, и
разойтись им нельзя.

`sslmode=disable` — соединение не выходит за пределы кластера
и шифруется тем, чем шифруется трафик в нём. Для базы на железе
это значение задаёт тот, кто её ставил: там дорога длиннее.
*/}}
{{- define "board.embeddedDatabaseURL" -}}
{{- $pg := .Values.postgresql -}}
{{- printf "postgres://%s:%s@%s:5432/%s?sslmode=disable" $pg.username $pg.password (include "board.postgresName" .) $pg.database -}}
{{- end -}}

{{- define "board.secretKey" -}}
{{- if and .Values.database.existingSecret (not .Values.postgresql.enabled) -}}
{{ .Values.database.existingSecretKey }}
{{- else -}}
DATABASE_URL
{{- end -}}
{{- end -}}

{{/*
Окружение одинаково у приложения и у задачи миграций: один образ,
одна конфигурация, разные подкоманды. Разойтись им нельзя — миграция,
поехавшая не в ту базу, обнаруживается позже всего.
*/}}
{{- define "board.env" -}}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ include "board.secretName" . }}
      key: {{ include "board.secretKey" . }}
- name: BASE_URL
  value: {{ required "нужен baseURL: из него собираются ссылки в приглашениях" .Values.baseURL | quote }}
- name: LISTEN_ADDR
  value: ":8080"
- name: SIGNUP
  value: {{ .Values.signup | quote }}
{{- with .Values.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}
