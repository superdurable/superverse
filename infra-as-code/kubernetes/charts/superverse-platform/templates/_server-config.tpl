{{/*
The server configuration document (cmd/superverse-server/config): the chart's own
settings fill what server.config leaves empty, and the chart then fixes the settings it is
the authority for. The ConfigMap, the Pod's rollout checksum and the Kind scripts all
read this one rendering. Secrets are never part of it; the server reads those from the
private Secret.
*/}}
{{- define "v2.serverConfig" -}}
{{- $product := include "v2.product" . -}}
{{- $config := deepCopy (default (dict) .Values.server.config) -}}
{{- $chart := dict
    "server" (dict "grpcAddress" ":8080" "healthAddress" ":8082" "blobCacheDir" "/var/cache/superverse/dex-blobs")
    "dex" (dict "serverAddress" (printf "%s-dex:8801" $product) "pull" (dict "maxConcurrentTasks" 256 "idleStreamsPerQueue" 1 "stopDrainTimeout" "60s")) -}}
{{- $config = merge $config $chart -}}
{{- /* The credential read endpoint's listener and the Service address applications reach it at, which configurations carry. */}}
{{- $connections := default (dict) (index $config "connections") -}}
{{- $_ := set $connections "readListen" (printf ":%s" (include "v2.credentialRead.port" .)) -}}
{{- $_ := set $connections "readURL" (printf "http://%s-server.%s.svc.cluster.local:%s/internal/connection-credentials/read" $product (include "v2.namespace" .) (include "v2.credentialRead.port" .)) -}}
{{- $_ := set $config "connections" $connections -}}
{{- $_ := set $config "environment" .Values.environment -}}
{{- $_ := set $config "localKind" .Values.localKind -}}
{{- if .Values.sandboxProfiles.codingProfile.enabled -}}
{{- $sandbox := default (dict) (index $config "sandbox") -}}
{{- $_ := set $sandbox "profile" (include "v2.codingProfile.configName" (include "v2.sandboxProfiles" . | fromJson)) -}}
{{- $_ := set $config "sandbox" $sandbox -}}
{{- end -}}
{{- toYaml $config -}}
{{- end -}}
