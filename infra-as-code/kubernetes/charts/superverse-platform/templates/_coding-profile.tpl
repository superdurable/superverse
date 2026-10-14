{{/* Content-addressed profiles retain old immutable ConfigMaps for admitted Sessions. */}}
{{- define "v2.codingProfile.name" -}}
{{- "coding-sandbox-profile" -}}
{{- end -}}

{{- define "v2.codingProfile.labels" -}}
app.kubernetes.io/name: {{ include "v2.codingProfile.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: {{ include "v2.product" $ }}
superdurable.io/catalog-authority: iac
helm.sh/chart: {{ printf "%s-platform-2.0.0" (include "v2.product" .) }}
{{- end -}}

{{/*
The immutable coding-v4 contract. Environments supply only the inputs in values.yaml
(sandboxProfiles.codingProfile); the contract always wins over them.
*/}}
{{- define "v2.codingProfile.contract" -}}
name: coding-v4
agentSandboxVersion: v1.0.1
# Sandbox image behavior revision; ordinary image rebuilds keep this value.
sandboxImageContractRevision: 8
sandboxName: coding
serviceAccountName: coding-sandbox
image:
  pullPolicy: IfNotPresent
# tini is PID 1: it reaps the orphans of detached Preview processes.
initBinaryPath: /usr/bin/tini
sandboxd:
  binaryPath: /usr/local/bin/sandboxd
  rootDir: /workspace
  restPort: 8080
  grpcPort: 9090
# The Sandbox-local Preview engine of superverse-previewctl. Its state lives on the
# workspace volume; Dex on dexPort is the only port the platform server reaches besides
# sandboxd. The preStop hook stops the engine within the termination grace period.
preview:
  controlBinaryPath: /usr/local/bin/superverse-previewctl
  stateDir: /var/lib/superverse/preview
  dexPort: 8801
  shutdownTimeout: 30s
terminationGracePeriodSeconds: 45
# Namespace-neutral guardrail templates published beside the Sandbox templates.
# Superverse injects only the session namespace and ownership labels.
namespaceBaseline:
  platformServer:
    podSelector:
      app: superverse-v2-server
    # The server's credential read endpoint, the one server port a Sandbox's Preview reaches.
    credentialReadPort: 8086
  quota:
    maxSandboxes: 1
    pods: 2
    services: 3
    persistentVolumeClaims: 1
    secrets: 4
    requestsCpu: "4"
    requestsMemory: 8Gi
    limitsCpu: "4"
    limitsMemory: 8Gi
    requestsEphemeralStorage: 20Gi
    limitsEphemeralStorage: 20Gi
    requestsStorage: 20Gi
  limits:
    container:
      defaultRequest: {cpu: 500m, memory: 1Gi, ephemeral-storage: 1Gi}
      default: {cpu: "2", memory: 4Gi, ephemeral-storage: 8Gi}
      min: {cpu: 50m, memory: 64Mi}
      max: {cpu: "4", memory: 8Gi, ephemeral-storage: 20Gi}
    persistentVolumeClaim:
      minStorage: 1Gi
      maxStorage: 20Gi
  publicHttpsEgress:
    cidr: 0.0.0.0/0
    except: [0.0.0.0/8, 10.0.0.0/8, 100.64.0.0/10, 127.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.0.0.0/24, 192.0.2.0/24,
             192.88.99.0/24, 192.168.0.0/16, 198.18.0.0/15, 198.51.100.0/24, 203.0.113.0/24, 224.0.0.0/4, 240.0.0.0/4]
securityContext:
  runAsUser: 1000
  runAsGroup: 1000
  fsGroup: 1000
resources:
  requests: {cpu: 500m, memory: 1Gi, ephemeral-storage: 1Gi}
  limits: {cpu: "2", memory: 4Gi, ephemeral-storage: 8Gi}
workspace:
  pvcName: workspace
  # One volume holds the repository checkout and the Preview engine state.
  repositorySubPath: repository
  previewStateSubPath: preview-state
tmp:
  sizeLimit: 2Gi
{{- end -}}

{{/*
sandboxProfiles with the contract merged in, the localKind settings set and the catalog
namespace filled in. The profile name hashes this exact dict with the metadata revision:
its keys and value types must not change, or every environment gets a new profile name.
*/}}
{{- define "v2.sandboxProfiles" -}}
{{- $local := .Values.localKind -}}
{{- $namespace := include "v2.namespace" . -}}
{{- $profiles := deepCopy .Values.sandboxProfiles -}}
{{- $coding := mergeOverwrite $profiles.codingProfile (include "v2.codingProfile.contract" . | fromYaml) -}}
{{/*
A disposable local Kind cluster has no gVisor, gp3, AWS Pod Identity or egress filtering, and
its standard storage does not enforce ReadWriteOncePod; it relies on the Preview state lock and
the platform's wait for Pod absence instead.
*/}}
{{- $_ := set $profiles "unsafeDevelopmentMode" $local -}}
{{- $_ := set $coding "podIdentityProjection" (not $local) -}}
{{- $_ := set $coding "sandboxClassName" (ternary "" "runsc" $local) -}}
{{- $_ := set $coding.namespaceBaseline "unsafeAllowAllEgress" $local -}}
{{- $_ := set $coding.workspace "storageClassName" (ternary "standard" "gp3" $local) -}}
{{- $_ := set $coding.workspace "accessMode" (ternary "ReadWriteOnce" "ReadWriteOncePod" $local) -}}
{{- $_ := set $coding.namespaceBaseline.platformServer "namespace" $namespace -}}
{{- $_ := set $profiles "codingProfile" $coding -}}
{{- $_ := set $profiles "catalogNamespace" $namespace -}}
{{- toJson $profiles -}}
{{- end -}}

{{/*
The revision of the profile's rendered data. The profile name hashes the values dict, not
the rendered ConfigMap data, so any change to the rendered profile bytes (contract.json, the
session baseline, pvc.yaml or sandbox.yaml) must bump this, and the revision that
infra-as-code/scripts/chart_validation.py expects; otherwise the immutable ConfigMap keeps its
name with new data and `helm upgrade` fails. Revision 2 names Flow-owned identities and the
merged platform server; revision 3 is coding-v4 with the Sandbox-local Preview engine;
revision 4 is coding-v4 with image contract revision 5, the command ledger and the file helper;
revision 5 is coding-v4 with image contract revision 6 and the application test engine;
revision 6 is coding-v4 with image contract revision 7, which ships the dex-connector-contributor skill;
revision 7 is coding-v4 with image contract revision 8, whose file helper refuses a path that
permissions deny as a failure of the call; revision 8 lets the Sandbox reach the platform
server's credential read endpoint; revision 9 matches the platform Service selector so
Preview credential reads work through the ClusterIP.
*/}}
{{- define "v2.codingProfile.metadataRevision" -}}9{{- end -}}

{{/* Called with the merged sandboxProfiles dict. */}}
{{- define "v2.codingProfile.configName" -}}
{{- $identity := deepCopy . -}}
{{- $_ := set $identity "metadataRevision" (include "v2.codingProfile.metadataRevision" . | int) -}}
{{- printf "coding-sandbox-profile-%s-%s" .codingProfile.name (toJson $identity | sha256sum | trunc 12) -}}
{{- end -}}
