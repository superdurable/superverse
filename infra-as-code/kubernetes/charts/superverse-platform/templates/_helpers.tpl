{{/* Generated catalog is read-only; operator environment selects one exact entry. */}}
{{- define "v2.names" -}}
{{- $catalog := .Files.Get "deployment-names.generated.json" | fromJson -}}
{{- if not (hasKey $catalog .Values.environment) }}{{ fail "invalid platform environment" }}{{ end -}}
{{- get $catalog .Values.environment | toJson -}}
{{- end -}}
{{- define "v2.product" -}}{{ (include "v2.names" . | fromJson).product }}{{- end -}}
{{- define "v2.workloadPrefix" -}}{{ (include "v2.names" . | fromJson).workload_prefix }}{{- end -}}
{{- define "v2.namespace" -}}{{ (include "v2.names" . | fromJson).platform_namespace }}{{- end -}}
{{- define "v2.studioHostname" -}}{{ required "hosted Studio hostname is required" .Values.studio.hostname }}{{- end -}}
{{- define "v2.domain" -}}{{ (include "v2.names" . | fromJson).dns_domain }}{{- end -}}
{{/*
The platform server's ServiceAccount. Fixed across environments: Go project scope provisioning
and Terraform IAM name it too.
*/}}
{{- define "v2.serverAccount" -}}superverse-server{{- end -}}
{{/* The platform server's Kubernetes identity, which every admission policy matches. */}}
{{- define "v2.serverUsername" -}}system:serviceaccount:{{ include "v2.namespace" . }}:{{ include "v2.serverAccount" . }}{{- end -}}
{{/* The matchCondition that limits an admission policy to the platform server's requests. */}}
{{- define "v2.serverOnlyMatch" -}}
- name: platform-server
  expression: request.userInfo.username == '{{ include "v2.serverUsername" . }}'
{{- end -}}
{{/* The Deny binding of the admission policy <product>-<name>. Pass (dict "root" $ "name" name). */}}
{{- define "v2.policyBinding" -}}
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: {{ include "v2.product" .root }}-{{ .name }}
  labels: {app.kubernetes.io/part-of: {{ include "v2.product" .root }}}
spec:
  policyName: {{ include "v2.product" .root }}-{{ .name }}
  validationActions: [Deny]
{{- end -}}
{{/* The namespaces the platform server may manage: a project's Live namespace and the namespace of its Session's Sandbox. */}}
{{- define "v2.projectNamespacePrefix" -}}{{ include "v2.workloadPrefix" . }}-project-{{- end -}}
{{- define "v2.sessionNamespacePrefix" -}}{{ include "v2.workloadPrefix" . }}-session-{{- end -}}
{{- define "v2.projectNamespacePattern" -}}^{{ include "v2.projectNamespacePrefix" . }}[a-z0-9]{1,16}${{- end -}}
{{- define "v2.sessionNamespacePattern" -}}^{{ include "v2.sessionNamespacePrefix" . }}[a-z0-9]{1,16}${{- end -}}
{{/*
CEL: the namespace is its project's Live namespace. Pass (dict "root" $ "namespace" <CEL string>
"labels" <CEL map of the ownership labels>).
*/}}
{{- define "v2.projectScopeNamespaceCEL" -}}
{{ .namespace }} == '{{ include "v2.projectNamespacePrefix" .root }}' + {{ .labels }}['superdurable.io/project-id']
{{- end -}}

{{/* Pod reads: the session manager waits until a Sandbox's Pods are gone. */}}
{{- define "v2.podReadRule" -}}
- apiGroups: [""]
  resources: [pods]
  verbs: [get, list]
{{- end -}}

{{/* Pod settings shared by the server, Studio and Dex. Pass (dict "root" $ "serviceAccount" name "automountToken" bool). */}}
{{- define "v2.platformPodSpec" -}}
{{- with .root.Values.platformNodeSelector }}
nodeSelector: {{ toYaml . | nindent 2 }}
{{- end }}
serviceAccountName: {{ .serviceAccount }}
automountServiceAccountToken: {{ .automountToken }}
enableServiceLinks: false
terminationGracePeriodSeconds: 45
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  seccompProfile: {type: RuntimeDefault}
{{- end -}}
{{- define "v2.platformContainerSecurity" -}}
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities: {drop: [ALL]}
{{- end -}}
{{- define "v2.platformScratchMounts" -}}
- {name: tmp, mountPath: /tmp}
- {name: cache, mountPath: /var/cache/superverse}
{{- end -}}
{{- define "v2.platformScratchVolumes" -}}
- name: tmp
  emptyDir: {sizeLimit: 256Mi}
- name: cache
  emptyDir: {sizeLimit: 2Gi}
{{- end -}}

{{/*
Local Kind only: the local Git host serves bare repositories from a directory of the Kind node, which
kind up mounts from the main checkout's .local/superverse-v2/git. The server Pod mounts it where the
server configuration says (localgit.dir), and the git-http port is the Service port that Sandboxes
clone from (localgit.listen).
*/}}
{{- define "v2.localGit.nodePath" -}}/var/lib/superverse-git{{- end -}}
{{- define "v2.localGit.mountPath" -}}/var/lib/superverse/git{{- end -}}
{{- define "v2.localGit.port" -}}8085{{- end -}}
{{- /* The server's credential read endpoint (cmd/superverse-server/config connections, infra.CredentialReadPort). */}}
{{- define "v2.credentialRead.port" -}}8086{{- end -}}

{{/* The Slack event relay's public events listener (connections.slackRelay.listenAddress; config.SlackEventsPort). */}}
{{- define "v2.slackEvents.port" -}}8087{{- end -}}

{{/* Dedicated Bridge listener (remoteAgent.listen; config.RemoteAgentPort). */}}
{{- define "v2.remoteAgent.port" -}}8088{{- end -}}

{{/* Platform images are deployed only by immutable digest. */}}
{{- define "v2.image" -}}{{ .repository }}@{{ required "platform images require an immutable digest" .digest }}{{- end -}}
{{/* Official NGINX unprivileged stable-alpine OCI index, resolved 2026-10-02. */}}
{{- define "v2.debugWebProxyImage" -}}
docker.io/nginxinc/nginx-unprivileged@sha256:ed04ec1ff34502c339ee5c3ae3f855442398edc1d05591e2b98981dcbbd20b1e
{{- end -}}
{{/*
The Envoy Gateway of the project routes: the namespace that holds its controller, its Gateway
"projects" and the proxies' Service, and the domain one label below which every project host lies.
Go derives the same names (internal/dexai/projects/infra/kube/addresses.go, infra.PublicRoutes.Host).
*/}}
{{- define "v2.gatewayNamespace" -}}{{ include "v2.product" . }}-gateway{{- end -}}
{{- define "v2.gatewayService" -}}projects-gateway{{- end -}}
{{- define "v2.projectDomain" -}}{{ if .Values.localKind }}localhost{{ else }}{{ .Values.environment }}.dexai.dev{{ end }}{{- end -}}
