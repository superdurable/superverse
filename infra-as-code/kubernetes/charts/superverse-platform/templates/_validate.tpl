{{/* Every chart input check; templates/validate.yaml runs it once on every render. */}}
{{- define "v2.validate" -}}
{{- range $key := list "BROWSER_SIGNING_KEY" "SUPERVERSE_COOKIE_SIGNING_KEY" -}}
{{- if hasKey $.Values.studio.config $key }}{{ fail "Signing keys belong in the component's private Secret" }}{{ end -}}
{{- end -}}
{{- range $key, $_ := .Values.server.config -}}
{{- if regexMatch "^[A-Z][A-Z0-9_]*$" $key }}{{ fail "server.config is the server configuration document: an environment variable name is not a setting, and secrets belong in the private Secret" }}{{ end -}}
{{- end -}}
{{- if .Values.sandboxProfiles.codingProfile.enabled -}}
{{- required "codingProfile.image.repository is required when codingProfile.enabled=true" .Values.sandboxProfiles.codingProfile.image.repository | trunc 0 -}}
{{- required "codingProfile.image.digest is required when codingProfile.enabled=true" .Values.sandboxProfiles.codingProfile.image.digest | trunc 0 -}}
{{- end -}}
{{- if not .Values.localKind -}}
{{- if eq (get (default (dict) .Values.server.config) "repohost" | toString) "localgit" }}{{ fail "the local Git host (repohost localgit) exists only in the local lanes" }}{{ end -}}
{{- $host := include "v2.studioHostname" . -}}
{{- if or (gt (len $host) 253) (not (regexMatch "^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$" $host)) }}{{ fail "hosted Studio requires an exact DNS hostname" }}{{ end -}}
{{- $origin := printf "https://%s" $host -}}
{{- if or (ne (dig "studio" "publicOrigin" "" .Values.server.config) $origin) (ne (get .Values.studio.config "SUPERVERSE_STUDIO_PUBLIC_ORIGIN") $origin) }}{{ fail "hosted Studio origins must match studio.hostname" }}{{ end -}}
{{- required "hosted TLS certificate is required" .Values.ingress.certificateArn | trunc 0 -}}
{{- if empty .Values.ingress.targetCIDRs }}{{ fail "platform ALB health checks require the ALB subnet CIDRs (ingress.targetCIDRs)" }}{{ end -}}
{{- range .Values.ingress.targetCIDRs -}}
{{- if or (eq . "0.0.0.0/0") (eq . "::/0") (not (contains "/" .)) }}{{ fail "platform ALB health checks require explicit ALB subnet CIDRs (ingress.targetCIDRs)" }}{{ end -}}
{{- end -}}
{{- if not (regexMatch (printf "^[0-9]{12}\\.dkr\\.ecr\\.[a-z0-9-]+\\.amazonaws\\.com/%s/applications$" ((include "v2.names" . | fromJson).ecr_prefix)) .Values.hostedProjects.applicationRepository) }}{{ fail "Hosted applications require the owned ECR repository" }}{{ end -}}
{{- end -}}
{{- if .Values.dex.debugIngress.enabled -}}
{{- if or .Values.localKind (not (has .Values.environment (list "test" "staging"))) }}{{ fail "platform Dex debug ingress is only available in hosted test/staging" }}{{ end -}}
{{- $authSecret := required "platform Dex Web auth Secret name is required" .Values.dex.debugIngress.webAuthSecretName -}}
{{- if or (gt (len $authSecret) 253) (not (regexMatch "^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$" $authSecret)) }}{{ fail "platform Dex Web auth Secret name must be a DNS subdomain" }}{{ end -}}
{{- required "platform Dex debug TLS certificate is required" .Values.dex.debugIngress.certificateArn | trunc 0 -}}
{{- if or (empty .Values.dex.debugIngress.subnetIDs) (empty .Values.dex.debugIngress.targetCIDRs) (empty .Values.dex.internalClientCIDRs) }}{{ fail "platform Dex debug ingress requires Cloud-owned ALB and internal-client subnets" }}{{ end -}}
{{- if ne (len .Values.dex.debugIngress.subnetIDs) (len .Values.dex.debugIngress.targetCIDRs) }}{{ fail "platform Dex debug ALB subnet identities and CIDRs must match" }}{{ end -}}
{{- range .Values.dex.debugIngress.subnetIDs -}}
{{- if not (regexMatch "^subnet-[0-9a-f]+$" .) }}{{ fail "platform Dex debug ALB requires exact subnet IDs" }}{{ end -}}
{{- end -}}
{{- range concat .Values.dex.internalClientCIDRs .Values.dex.debugIngress.targetCIDRs -}}
{{- if or (eq . "0.0.0.0/0") (eq . "::/0") (not (contains "/" .)) }}{{ fail "platform Dex pod admission requires explicit internal and ALB subnet CIDRs" }}{{ end -}}
{{- end -}}
{{- end -}}
{{- if and (gt (int .Values.dex.replicas) 1) (not .Values.dex.sharedStreamStore) }}{{ fail "platform Dex runs more than one replica only with a shared Stream store (dex.sharedStreamStore): the memory store is per process" }}{{ end -}}
{{- if and .Values.localKind (ne (int .Values.server.replicas) 1) }}{{ fail "Local Kind runs one server Pod: the local Git host's repository directory belongs to one process" }}{{ end -}}
{{- if lt (int .Values.studio.replicas) 1 }}{{ fail "studio.replicas must be at least 1" }}{{ end -}}
{{- required "server image repository is required" .Values.server.image.repository | trunc 0 -}}
{{- required "Dex Temporal address is required" .Values.dex.temporalAddress | trunc 0 -}}
{{- end -}}
