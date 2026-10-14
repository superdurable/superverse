{{/*
IaC-owned, namespace-neutral coding profile session baseline templates, each called with
(dict "root" $ "profile" <merged codingProfile>).

Superverse injects only the session namespace name/namespace and ownership labels. The
policy bodies are part of the immutable coding profile: any change to their rendered bytes
must bump v2.codingProfile.metadataRevision. Keep the static assertions in
infra-as-code/scripts/chart_validation.py aligned with these templates.
*/}}

{{- define "v2.codingProfile.sessionBaselineLabels" -}}
app.kubernetes.io/name: {{ include "v2.product" .root }}-agent-sandbox-session-baseline
app.kubernetes.io/managed-by: {{ include "v2.product" .root }}
app.kubernetes.io/part-of: {{ include "v2.product" .root }}
superdurable.io/sandbox-profile: {{ .profile.name | quote }}
{{- end -}}

{{- define "v2.codingProfile.sessionBaselineNamespace" -}}
apiVersion: v1
kind: Namespace
metadata:
  labels:
    {{- include "v2.codingProfile.sessionBaselineLabels" . | nindent 4 }}
    superdurable.io/purpose: coding-sandbox-sessions
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/enforce-version: v1.35
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/audit-version: v1.35
    pod-security.kubernetes.io/warn: restricted
    pod-security.kubernetes.io/warn-version: v1.35
{{- end -}}

{{- define "v2.codingProfile.sessionBaselineManagerRoleBinding" -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: {{ include "v2.product" .root }}-session-manager
  labels:
    {{- include "v2.codingProfile.sessionBaselineLabels" . | nindent 4 }}
subjects:
  - kind: ServiceAccount
    name: {{ include "v2.serverAccount" . }}
    namespace: {{ include "v2.namespace" .root }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ include "v2.product" .root }}-session-manager
{{- end -}}

{{- define "v2.codingProfile.sessionBaselineServiceAccount" -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .profile.serviceAccountName }}
  labels:
    {{- include "v2.codingProfile.sessionBaselineLabels" . | nindent 4 }}
automountServiceAccountToken: false
{{- end -}}

{{- define "v2.codingProfile.sessionBaselineResourceQuota" -}}
apiVersion: v1
kind: ResourceQuota
metadata:
  name: coding-sandbox-quota
  labels:
    {{- include "v2.codingProfile.sessionBaselineLabels" . | nindent 4 }}
spec:
  hard:
    count/sandboxes.agents.x-k8s.io: {{ .profile.namespaceBaseline.quota.maxSandboxes | quote }}
    pods: {{ .profile.namespaceBaseline.quota.pods | quote }}
    services: {{ .profile.namespaceBaseline.quota.services | quote }}
    persistentvolumeclaims: {{ .profile.namespaceBaseline.quota.persistentVolumeClaims | quote }}
    secrets: {{ .profile.namespaceBaseline.quota.secrets | quote }}
    requests.cpu: {{ .profile.namespaceBaseline.quota.requestsCpu | quote }}
    requests.memory: {{ .profile.namespaceBaseline.quota.requestsMemory | quote }}
    limits.cpu: {{ .profile.namespaceBaseline.quota.limitsCpu | quote }}
    limits.memory: {{ .profile.namespaceBaseline.quota.limitsMemory | quote }}
    requests.ephemeral-storage: {{ .profile.namespaceBaseline.quota.requestsEphemeralStorage | quote }}
    limits.ephemeral-storage: {{ .profile.namespaceBaseline.quota.limitsEphemeralStorage | quote }}
    requests.storage: {{ .profile.namespaceBaseline.quota.requestsStorage | quote }}
{{- end -}}

{{- define "v2.codingProfile.sessionBaselineLimitRange" -}}
apiVersion: v1
kind: LimitRange
metadata:
  name: coding-sandbox-limits
  labels:
    {{- include "v2.codingProfile.sessionBaselineLabels" . | nindent 4 }}
spec:
  limits:
    - type: Container
      defaultRequest:
        {{- toYaml .profile.namespaceBaseline.limits.container.defaultRequest | nindent 8 }}
      default:
        {{- toYaml .profile.namespaceBaseline.limits.container.default | nindent 8 }}
      min:
        {{- toYaml .profile.namespaceBaseline.limits.container.min | nindent 8 }}
      max:
        {{- toYaml .profile.namespaceBaseline.limits.container.max | nindent 8 }}
    - type: PersistentVolumeClaim
      min:
        storage: {{ .profile.namespaceBaseline.limits.persistentVolumeClaim.minStorage | quote }}
      max:
        storage: {{ .profile.namespaceBaseline.limits.persistentVolumeClaim.maxStorage | quote }}
{{- end -}}

{{- define "v2.codingProfile.sessionBaselineDefaultDeny" -}}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
  labels:
    {{- include "v2.codingProfile.sessionBaselineLabels" . | nindent 4 }}
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
  {{- if .profile.namespaceBaseline.unsafeAllowAllEgress }}
  # UNSAFE LOCAL TEST ONLY: retain ingress isolation while allowing package
  # Local unsafe mode permits direct egress for disposable development clusters.
  egress:
    - {}
  {{- end }}
{{- end -}}

{{- define "v2.codingProfile.sessionBaselineSuperverseIngress" -}}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-{{ include "v2.product" .root }}-to-sandboxd
  labels:
    {{- include "v2.codingProfile.sessionBaselineLabels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      superdurable.io/sandbox-profile: {{ .profile.name | quote }}
  policyTypes:
    - Ingress
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: {{ include "v2.namespace" .root }}
          podSelector:
            matchLabels:
              {{- toYaml .profile.namespaceBaseline.platformServer.podSelector | nindent 14 }}
      ports:
        - protocol: TCP
          port: {{ .profile.sandboxd.restPort }}
        - protocol: TCP
          port: {{ .profile.sandboxd.grpcPort }}
        - protocol: TCP
          port: {{ .profile.preview.dexPort }}
{{- end -}}

{{- define "v2.codingProfile.sessionBaselineSandboxEgress" -}}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-sandbox-dns-and-public-https
  labels:
    {{- include "v2.codingProfile.sessionBaselineLabels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      superdurable.io/sandbox-profile: {{ .profile.name | quote }}
  policyTypes:
    - Egress
  egress:
    {{- if .profile.podIdentityProjection }}
    - to:
        - ipBlock: {cidr: 169.254.170.23/32}
      ports: [{protocol: TCP, port: 80}]
    {{- end }}
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: {{ include "v2.namespace" .root }}
          podSelector:
            matchLabels:
              {{- toYaml .profile.namespaceBaseline.platformServer.podSelector | nindent 14 }}
      ports:
        - protocol: TCP
          port: {{ .profile.namespaceBaseline.platformServer.credentialReadPort }}
    - to:
        - ipBlock:
            cidr: {{ .profile.namespaceBaseline.publicHttpsEgress.cidr }}
            except:
              {{- toYaml .profile.namespaceBaseline.publicHttpsEgress.except | nindent 14 }}
      ports:
        - protocol: TCP
          port: 443
{{- end -}}
