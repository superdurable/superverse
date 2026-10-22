// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package config is the operator configuration of the Superverse server: the
// one schema of every non-secret setting, its defaults, and the strict loader
// of the YAML files that carry the values. Secrets never appear here; the
// server reads them from its environment (see README.md).
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"gopkg.in/yaml.v3"
)

// PlatformDexWorkerStreamCap bounds retained streams per app Worker ID on each matching owner.
const PlatformDexWorkerStreamCap int = 4224

// Defaults of the settings that have one. A setting without a default is
// required, or empty where its section says so.
const (
	DefaultGRPCAddress                     = ":8080"
	DefaultHealthAddress                   = ":8082"
	DefaultBlobCacheDir                    = "/tmp/superverse-blob-cache"
	DefaultProjectIDLength                 = 4
	DefaultDexServerAddress                = "127.0.0.1:8801"
	DefaultDexPullMaxConcurrentTasks       = 256
	DefaultDexPullIdleStreamsPerQueue      = 1
	DefaultDexPullStopDrainTimeout         = 60 * time.Second
	DefaultEffort                          = conversation.EffortMedium
	DefaultSubagentInvestigate             = conversation.EffortLow
	DefaultSubagentChange                  = conversation.EffortMedium
	DefaultSubagentConnectorDevelop        = conversation.EffortHigh
	DefaultCompactionTokens                = 200_000
	DefaultThinkingPrefixMismatch          = conversation.PrefixMismatchDropBlock
	DefaultTaskTurns                       = 5
	DefaultPlatformTurns                   = 3
	DefaultClosestExamplePick              = false
	DefaultSandboxHold                     = time.Hour
	DefaultSandboxPreviewHold              = 7 * 24 * time.Hour
	DefaultSandboxProfile                  = "coding-sandbox-profile-" + infra.CodingSandboxProfile
	DefaultLocalBuildURL                   = "http://host.docker.internal:29792"
	DefaultGitHubOwner                     = "superdurable-apps"
	DefaultTemporalRetentionDays           = 30
	DefaultTemporalAddress                 = "xtemporal-frontend.temporal.svc.cluster.local:7233"
	DefaultRuntime                         = RuntimeKubernetes
	DefaultLocalRoutePort                  = 23080
	DefaultPostgresPort                    = 5432
	DefaultPostgresDatabase                = "superverse"
	DefaultPostgresSSLMode                 = PostgresSSLVerifyFull
	DefaultPostgresMaxConnections          = 16
	DefaultConnectionsReadListen           = ":8086"
	DefaultSlackRelayListen                = ":8087"
	DefaultRemoteAgentListen               = ":8088"
	DefaultRemoteAgentMaxLongPolls         = 1000
	DefaultRemoteAgentMaxConcurrentReads   = 4
	DefaultRemoteAgentMaxConcurrentReports = 16
	DefaultMinimumBridgeVersion            = "0.1.0"
	DefaultBridgeDownloadsDir              = "/opt/superverse/bridge"
	DefaultSlackRelayQueue                 = eventqueue.ImplementationMemory
	DefaultSlackRelayMaxEvents             = 10_000
	DefaultSlackRelayMaxBytes              = 64 << 20
	DefaultSlackRelayMaxEventBytes         = 1 << 20
	DefaultSlackRelayMaxScopeEvents        = 100
	DefaultSlackRelayTimeout               = 10 * time.Second
	DefaultSlackRelayConcurrency           = 16
	DefaultRunsDexConnections              = 128
	DefaultLiveRetirementDrain             = 10 * time.Minute
	DefaultBuildLogReadsPerSecond          = 2.0
	MaxPlatformAdminEmails                 = 20
	DefaultPlatformAdminEmail              = "admin@superverse.test"
	DefaultAttachmentsPerMessage           = 10
	DefaultAttachmentImageLongEdge         = 2000
	DefaultAttachmentImageMaxBytes         = 1 << 20
	DefaultAttachmentDocumentMaxBytes      = 20 << 20
	DefaultAttachmentJSONMaxBytes          = 4 << 20
	DefaultAttachmentMessageInlineText     = 120 << 10
	DefaultAttachmentSegmentBytes          = 20 << 20
	DefaultAttachmentSegmentImages         = 60
	DefaultAttachmentUploadWait            = time.Hour
	DefaultAttachmentStagingRetention      = 24 * time.Hour
	DefaultAttachmentTeamSpaceStagedFiles  = 200
	DefaultAttachmentTeamSpaceStagedBytes  = 2 << 30
	DefaultAttachmentProjectLifetimeFiles  = 1000
	DefaultAttachmentProjectLifetimeBytes  = 2 << 30
	DefaultAttachmentRenditionCacheBytes   = 256 << 20
)

// SlackEventsPort is the server's port of the public Slack events listener
// in the cluster: the chart's container and Service port, which the
// platform load balancer routes POST /api/slack/events to.
const SlackEventsPort = 8087

// RemoteAgentPort is the dedicated Bridge listener port routed by the chart.
const RemoteAgentPort = 8088

// LocalKindRemoteAgentPort is the loopback forward in the Kind FORWARDS table.
const LocalKindRemoteAgentPort = 28089

// Runtime is where the server runs project workloads: Sandboxes, Previews and
// Live applications with their storage, Temporal and Dex.
type Runtime string

// The runtimes.
const (
	// RuntimeKubernetes runs them in the server's cluster: Local Kind and the
	// hosted environments.
	RuntimeKubernetes Runtime = "kubernetes"
	// RuntimeDocker runs them as containers of the local Docker Engine: Local
	// Native.
	RuntimeDocker Runtime = "docker"
)

// Validate admits the declared runtimes.
func (r Runtime) Validate() error {
	if r != RuntimeKubernetes && r != RuntimeDocker {
		return fmt.Errorf("runtime %q is neither %s nor %s", r, RuntimeKubernetes, RuntimeDocker)
	}
	return nil
}

// Config is the complete non-secret configuration of one server process.
type Config struct {
	// Environment selects the deployment-name catalog entry: test, staging or prod.
	Environment string `yaml:"environment"`
	// LocalKind marks the local lanes, Local Kind and Local Native, whose
	// projects run their own MinIO and Temporal; only the test environment.
	LocalKind bool `yaml:"localKind"`
	// Runtime is where project workloads run: kubernetes, or docker in Local
	// Native.
	Runtime Runtime `yaml:"runtime"`

	// RepositoryHost selects the Git host: github, or localgit in the local lanes.
	RepositoryHost string `yaml:"repohost"`

	Server       ServerConfig       `yaml:"server"`
	Accounts     AccountsConfig     `yaml:"accounts"`
	Dex          DexConfig          `yaml:"dex"`
	Studio       StudioConfig       `yaml:"studio"`
	Agent        AgentConfig        `yaml:"agent"`
	Sandbox      SandboxConfig      `yaml:"sandbox"`
	Storage      StorageConfig      `yaml:"storage"`
	Attachments  AttachmentsConfig  `yaml:"attachments"`
	Postgres     PostgresConfig     `yaml:"postgres"`
	Connections  ConnectionsConfig  `yaml:"connections"`
	RemoteAgent  RemoteAgentConfig  `yaml:"remoteAgent"`
	Builds       BuildsConfig       `yaml:"builds"`
	GitHub       GitHubConfig       `yaml:"github"`
	LocalGit     LocalGitConfig     `yaml:"localgit"`
	Live         LiveConfig         `yaml:"live"`
	AWS          AWSConfig          `yaml:"aws"`
	ProjectCloud ProjectCloudConfig `yaml:"projectCloud"`
	Routes       RoutesConfig       `yaml:"routes"`
	Native       NativeConfig       `yaml:"native"`
}

// AccountsConfig selects the ordinary account with platform-wide authority.
type AccountsConfig struct {
	PlatformAdminEmails []string `yaml:"platformAdminEmails"`
}

// RemoteAgentConfig is the dedicated Bridge listener and its approved bounds.
type RemoteAgentConfig struct {
	Listen               string `yaml:"listen"`
	MaxLongPolls         int    `yaml:"maxLongPolls"`
	MaxConcurrentReads   int    `yaml:"maxConcurrentReads"`
	MaxConcurrentReports int    `yaml:"maxConcurrentReports"`
	MinimumBridgeVersion string `yaml:"minimumBridgeVersion"`
	AcceptDevBridge      bool   `yaml:"acceptDevBridge"`
	DownloadsDir         string `yaml:"downloadsDir"`
}

// ServerConfig is the process's listeners and request-level limits.
type ServerConfig struct {
	GRPCAddress   string `yaml:"grpcAddress"`
	HealthAddress string `yaml:"healthAddress"`
	// RPCCoverageFile records served methods only in a local test lane.
	RPCCoverageFile string `yaml:"rpcCoverageFile"`
	// BlobCacheDir holds the Dex payload blob cache; it is exclusive to this process.
	BlobCacheDir string `yaml:"blobCacheDir"`
	// ProjectIDLength is the length of a new Project ID.
	ProjectIDLength int `yaml:"projectIDLength"`
}

// DexConfig is how the server reaches the platform Dex and how Dex reaches this process's Worker.
type DexConfig struct {
	ServerAddress string        `yaml:"serverAddress"`
	Pull          DexPullConfig `yaml:"pull"`
}

// DexPullConfig bounds this process's outbound Worker task streams and drain.
type DexPullConfig struct {
	MaxConcurrentTasks  int           `yaml:"maxConcurrentTasks"`
	IdleStreamsPerQueue int           `yaml:"idleStreamsPerQueue"`
	StopDrainTimeout    time.Duration `yaml:"stopDrainTimeout"`
}

// StudioConfig is the Studio the server returns OAuth actors to, and the
// Studio Runs it serves.
type StudioConfig struct {
	// PublicOrigin is the http or https origin of Studio, without a path.
	PublicOrigin string `yaml:"publicOrigin"`
	// RunsDexConnections bounds the verified Dex connections that Studio Runs
	// pools on this replica.
	RunsDexConnections int `yaml:"runsDexConnections"`
}

// AgentConfig is the Session Agent's model and turn settings.
type AgentConfig struct {
	// Model is the model a new Session starts with: a Claude model, or mock
	// with MockModel.
	Model conversation.Model `yaml:"model"`
	// Effort is the base effort of every model call, clamped per model.
	Effort conversation.Effort `yaml:"effort"`
	// SubagentInvestigateEffort, SubagentChangeEffort and
	// SubagentConnectorDevelopEffort are the base efforts of the model calls of
	// an investigate, a change and a connector_develop subagent, clamped per
	// model. A connector_select subagent runs at medium effort, fixed.
	SubagentInvestigateEffort      conversation.Effort `yaml:"subagentInvestigateEffort"`
	SubagentChangeEffort           conversation.Effort `yaml:"subagentChangeEffort"`
	SubagentConnectorDevelopEffort conversation.Effort `yaml:"subagentConnectorDevelopEffort"`
	// CompactionTokens is the context size that triggers a compaction.
	CompactionTokens int `yaml:"compactionTokens"`
	// ThinkingPrefixMismatch is what the API does with a replayed thinking block
	// that fails its conversation check: drop_block or error.
	ThinkingPrefixMismatch conversation.PrefixMismatchBehavior `yaml:"thinkingPrefixMismatch"`
	// TaskTurns is how many consecutive turns task notifications open before
	// automatic continuation pauses until the user replies.
	TaskTurns int `yaml:"taskTurns"`
	// PlatformTurns is how many consecutive turns application-owned delivery
	// failures of the Session's HEAD open before automatic continuation
	// pauses until the user replies; 0 opens none.
	PlatformTurns int `yaml:"platformTurns"`
	// MockModel starts the in-process mock model and offers it to every
	// Session as the model mock, besides the Claude models; model may then be
	// mock. Refused in prod.
	MockModel bool `yaml:"mockModel"`
	// ClosestExamplePick lets CreateProject start a Project created from a
	// first message, with no example picked, from the example the server finds
	// closest to that message; off, it starts from the default example.
	ClosestExamplePick bool `yaml:"closestExamplePick"`
}

// SandboxConfig is the lifetime of a Session's Sandbox, which the Session owns
// and reclaims at its deadline.
type SandboxConfig struct {
	// Hold is how long the Sandbox stays after the main agent or a Preview
	// command last used it.
	Hold time.Duration `yaml:"hold"`
	// PreviewHold is how long the Sandbox stays after a Preview became READY
	// on it, which a ready delivery notification tells the Session.
	PreviewHold time.Duration `yaml:"previewHold"`
	// Profile is the installed immutable coding profile. The chart and the Kind
	// deploy supply the content-addressed name of the one they install.
	Profile string `yaml:"profile"`
}

// PostgresConfig is the database of the accounts and the read model
// (docs/architecture/teamspace/infrastructure.md). Its roles have fixed names;
// their passwords are secrets.
type PostgresConfig struct {
	Host     string          `yaml:"host"`
	Port     int             `yaml:"port"`
	Database string          `yaml:"database"`
	SSLMode  PostgresSSLMode `yaml:"sslMode"`
	// MaxConnections bounds the server's pool.
	MaxConnections int `yaml:"maxConnections"`
}

// PostgresSSLMode is how a connection uses TLS.
type PostgresSSLMode string

// The admitted modes: disable only on the local lanes, which run PostgreSQL
// beside the server.
const (
	PostgresSSLDisable    PostgresSSLMode = "disable"
	PostgresSSLRequire    PostgresSSLMode = "require"
	PostgresSSLVerifyFull PostgresSSLMode = "verify-full"
)

// Validate admits the declared modes.
func (m PostgresSSLMode) Validate() error {
	switch m {
	case PostgresSSLDisable, PostgresSSLRequire, PostgresSSLVerifyFull:
		return nil
	}
	return fmt.Errorf("sslMode %q is not %s, %s or %s", m, PostgresSSLDisable, PostgresSSLRequire, PostgresSSLVerifyFull)
}

// ConnectionsConfig is the TeamSpace connections' credential read endpoint,
// where the platform's provider calls may connect, and the OAuth clients the
// platform registered (docs/architecture/teamspace/connections.md).
type ConnectionsConfig struct {
	// ReadListen is the address the credential read endpoint binds, apart
	// from every other listener.
	ReadListen string `yaml:"readListen"`
	// ReadURL is the endpoint's URL as applications reach it. Configuration
	// documents carry it, and a frozen Live version keeps it, so it is fixed
	// per environment.
	ReadURL string `yaml:"readURL"`
	// OAuthCallbackOrigin is the origin of the OAuth redirect URI that
	// providers return actors to, /api/connector-oauth/callback; empty is
	// studio.publicOrigin. A developer's manual test sets a tunnel host whose
	// proxy redirects the callback to the local Studio.
	OAuthCallbackOrigin string `yaml:"oauthCallbackOrigin"`
	// AllowInsecureOAuthEndpoints lets provider calls use plain http to
	// loopback and private addresses, such as the local OAuth server.
	AllowInsecureOAuthEndpoints bool `yaml:"allowInsecureOAuthEndpoints"`
	// PlatformClients are the OAuth applications the platform registered,
	// by "<connector>/<method>"; their secrets are environment secrets.
	PlatformClients map[string]PlatformOAuthClient `yaml:"platformClients"`
	// SlackRelay relays the platform Slack application's events to the
	// applications whose platform Slack connections serve the workspace.
	SlackRelay SlackRelayConfig `yaml:"slackRelay"`
}

// SlackRelayConfig is the Slack event relay: its public listener, the
// in-memory queue's limits and the deliveries to applications. The Signing
// Secret is the secret SUPERVERSE_SLACK_SIGNING_SECRET.
type SlackRelayConfig struct {
	// ListenAddress is the public events listener, which serves only
	// POST /api/slack/events, and only with a Signing Secret.
	ListenAddress string                `yaml:"listenAddress"`
	Queue         SlackRelayQueueConfig `yaml:"queue"`
	Delivery      SlackRelayDelivery    `yaml:"delivery"`
}

// SlackRelayQueueConfig selects the queue implementation and bounds one
// replica's queued events.
type SlackRelayQueueConfig struct {
	Implementation eventqueue.Implementation `yaml:"implementation"`
	MaxEvents      int                       `yaml:"maxEvents"`
	MaxBytes       int                       `yaml:"maxBytes"`
	// MaxEventBytes bounds one event and the events endpoint's body.
	MaxEventBytes  int `yaml:"maxEventBytes"`
	MaxScopeEvents int `yaml:"maxScopeEvents"`
}

// SlackRelayDelivery bounds the POSTs to applications.
type SlackRelayDelivery struct {
	Timeout     time.Duration `yaml:"timeout"`
	Concurrency int           `yaml:"concurrency"`
}

// PlatformOAuthClient is the non-secret part of a platform OAuth
// application: its client ID and the scopes it is verified for.
type PlatformOAuthClient struct {
	ClientID string   `yaml:"clientId"`
	Scopes   []string `yaml:"scopes"`
}

// StorageConfig is the release artifact bucket.
type StorageConfig struct {
	// S3Endpoint points S3 at an endpoint of its own. Local Kind only.
	S3Endpoint string `yaml:"s3Endpoint"`
	// BrowserEndpoint is the storage endpoint that browsers reach, which the
	// presigned uploads and downloads name; empty is the AWS regional endpoint.
	BrowserEndpoint  string `yaml:"browserEndpoint"`
	ArtifactBucket   string `yaml:"artifactBucket"`
	ArtifactKMSKeyID string `yaml:"artifactKMSKeyID"`
}

// AttachmentsConfig bounds the files people attach to messages
// (docs/architecture/attachments/): their sizes, the share of a model request
// they take, how long staged uploads live and the quotas that count them.
type AttachmentsConfig struct {
	// MaxPerMessage bounds the attachments of one message.
	MaxPerMessage int `yaml:"maxPerMessage"`
	// ImageLongEdge and ImageMaxBytes bound an image, which Studio normalizes
	// to them before it uploads.
	ImageLongEdge int   `yaml:"imageLongEdge"`
	ImageMaxBytes int64 `yaml:"imageMaxBytes"`
	// DocumentMaxBytes and JSONMaxBytes bound an uploaded document and JSON file.
	DocumentMaxBytes int64 `yaml:"documentMaxBytes"`
	JSONMaxBytes     int64 `yaml:"jsonMaxBytes"`
	// MessageInlineText bounds the document text one message renders inline.
	MessageInlineText int `yaml:"messageInlineText"`
	// SegmentBytes and SegmentImages bound the rendered attachment blocks of
	// one context segment, which every model call of the segment resends.
	SegmentBytes  int64 `yaml:"segmentBytes"`
	SegmentImages int   `yaml:"segmentImages"`
	// UploadWait is how long a registered upload waits for its PUT, and
	// StagingRetention how long a prepared attachment stays staged unsent.
	UploadWait       time.Duration `yaml:"uploadWait"`
	StagingRetention time.Duration `yaml:"stagingRetention"`
	// TeamSpaceStagedFiles and TeamSpaceStagedBytes bound a TeamSpace's staged
	// attachments; ProjectLifetimeFiles and ProjectLifetimeBytes the
	// attachments a Project's messages send over its lifetime.
	TeamSpaceStagedFiles int   `yaml:"teamSpaceStagedFiles"`
	TeamSpaceStagedBytes int64 `yaml:"teamSpaceStagedBytes"`
	ProjectLifetimeFiles int   `yaml:"projectLifetimeFiles"`
	ProjectLifetimeBytes int64 `yaml:"projectLifetimeBytes"`
	// RenditionCacheBytes bounds this replica's cache of rendition objects.
	RenditionCacheBytes int64 `yaml:"renditionCacheBytes"`
}

// BuildsConfig is where Releases are built.
type BuildsConfig struct {
	// LocalURL is the host build service of Local Kind and Native.
	LocalURL string `yaml:"localURL"`
	// Hosted is the CodeBuild project of every other environment.
	Hosted HostedBuildConfig `yaml:"hosted"`
	// LogReadsPerSecond bounds the build log reads one replica starts, which
	// its viewers share.
	LogReadsPerSecond float64 `yaml:"logReadsPerSecond"`
}

// HostedBuildConfig is the CodeBuild project that builds Releases outside Local Kind.
type HostedBuildConfig struct {
	Project    string `yaml:"project"`
	Repository string `yaml:"repository"`
	Bucket     string `yaml:"bucket"`
	Prefix     string `yaml:"prefix"`
	KMSKeyARN  string `yaml:"kmsKeyARN"`
	ToolImage  string `yaml:"toolImage"`
	HostImage  string `yaml:"hostImage"`
}

// GitHubConfig names the GitHub App installation. The IDs are not secrets; the
// private key is, and comes from the environment.
type GitHubConfig struct {
	Owner          string `yaml:"owner"`
	AppID          int64  `yaml:"appId"`
	InstallationID int64  `yaml:"installationId"`
	// Issues is where the platform files its incidents, through another
	// installation of the same App; without it they are only logged.
	Issues GitHubIssuesConfig `yaml:"issues"`
	// Connectors is the upstream connectors library that connector_develop
	// subagents contribute to, through another installation of the same App;
	// without it no contribution branch reaches GitHub.
	Connectors GitHubConnectorsConfig `yaml:"connectors"`
}

// GitHubConnectorsConfig is the upstream connectors library and the App's
// installation on it: both set, or both empty, the default. Hosted
// environments name superdurable/dex-connectors-library; tests and the
// acceptance profile its copy superdurable/dex-connectors-library-test.
type GitHubConnectorsConfig struct {
	// Repository is owner/name.
	Repository     string `yaml:"repository"`
	InstallationID int64  `yaml:"installationId"`
}

// GitHubIssuesConfig is the repository of the platform's incident issues and
// the App's installation on it: both set, or both empty, the default.
type GitHubIssuesConfig struct {
	// Repository is owner/name.
	Repository     string `yaml:"repository"`
	InstallationID int64  `yaml:"installationId"`
}

// LocalGitConfig is the local Git host that runs inside the server when
// repohost is localgit. The signing key of its credentials is a secret and comes
// from the environment.
type LocalGitConfig struct {
	// Dir holds the bare repositories. Local Kind mounts the main checkout's
	// .local/superverse-v2/git there; Native's launcher sets its own
	// .local/superverse-v2/native/git.
	Dir string `yaml:"dir"`
	// Listen is the address of the Git HTTP server.
	Listen string `yaml:"listen"`
	// CloneBaseURL is the origin a Sandbox clones from, without a path: the
	// server Service in Local Kind, the host server through
	// host.docker.internal in Native.
	CloneBaseURL string `yaml:"cloneBaseURL"`
}

// LiveConfig is the images of every project scope, each pinned by digest.
type LiveConfig struct {
	DexImage string `yaml:"dexImage"`
	// TemporalImage and S3Image run inside each project scope in the local
	// lanes only.
	TemporalImage string `yaml:"temporalImage"`
	S3Image       string `yaml:"s3Image"`
	// RetirementDrain is how long a replaced Live generation keeps running
	// after the new one committed before it is retired.
	RetirementDrain time.Duration `yaml:"retirementDrain"`
}

// AWSConfig is the AWS account of the server. Credentials come from the
// environment or the pod identity, never from here.
type AWSConfig struct {
	Region    string `yaml:"region"`
	AccountID string `yaml:"accountId"`
}

// ProjectCloudConfig is the project cloud account of every environment but Local Kind.
type ProjectCloudConfig struct {
	EKSClusterName         string `yaml:"eksClusterName"`
	BucketPrefix           string `yaml:"bucketPrefix"`
	KMSKeyARN              string `yaml:"kmsKeyARN"`
	PermissionsBoundaryARN string `yaml:"permissionsBoundaryARN"`
	TemporalAddress        string `yaml:"temporalAddress"`
	TemporalRetentionDays  int32  `yaml:"temporalRetentionDays"`
}

// RoutesConfig are the public routes of the local lanes: every application at
// <scope>-<project>.localhost:<localPort>, through one gateway.
type RoutesConfig struct {
	// LocalPort is the port of the public application URLs.
	LocalPort int `yaml:"localPort"`
	// GatewayURL is where the server probes a route, with the route's Host header.
	GatewayURL string `yaml:"gatewayURL"`
}

// NativeConfig is Local Native, runtime docker: the server, Studio and the
// Release builder run on the host and every project workload in the local
// Docker Engine. The launcher writes the run's fields into a last layer.
type NativeConfig struct {
	// Instance identifies the isolated Local Native instance that the launcher
	// owns. It scopes every Docker object the server creates.
	Instance string `yaml:"instance"`
	// RunID labels the health answers of this process, so the launcher accepts
	// readiness only from the run it started.
	RunID string `yaml:"runId"`
	// StateDir is the lane's private state directory: the route files, the
	// credentials the server generates and the rendered coding profiles.
	StateDir string `yaml:"stateDir"`
	// DockerSocket is the unix socket of the Docker Engine API.
	DockerSocket string `yaml:"dockerSocket"`
	// NetworkPrefix is the IPv4 network from which every Docker network of the
	// lane takes a /24 subnet.
	NetworkPrefix string `yaml:"networkPrefix"`
	// Registry is the host:port of the local image registry, under which the
	// engine pushes and pulls the Release images alike.
	Registry string `yaml:"registry"`
	// GatewayAPI is the origin of the gateway's API, which confirms that a
	// route is loaded.
	GatewayAPI string `yaml:"gatewayAPI"`
	// RoutesListen is the loopback address where the server serves the
	// gateway its routes.
	RoutesListen string `yaml:"routesListen"`
}

// Default is the configuration before any file: every default applied.
func Default() Config {
	return Config{
		Environment:    deploymentnames.EnvironmentTest.String(),
		Runtime:        DefaultRuntime,
		RepositoryHost: repohost.ProviderGitHub.String(),
		Accounts:       AccountsConfig{PlatformAdminEmails: []string{DefaultPlatformAdminEmail}},
		Server: ServerConfig{
			GRPCAddress: DefaultGRPCAddress, HealthAddress: DefaultHealthAddress, BlobCacheDir: DefaultBlobCacheDir,
			ProjectIDLength: DefaultProjectIDLength,
		},
		Dex:    DexConfig{ServerAddress: DefaultDexServerAddress, Pull: DexPullConfig{MaxConcurrentTasks: DefaultDexPullMaxConcurrentTasks, IdleStreamsPerQueue: DefaultDexPullIdleStreamsPerQueue, StopDrainTimeout: DefaultDexPullStopDrainTimeout}},
		Studio: StudioConfig{RunsDexConnections: DefaultRunsDexConnections},
		Agent: AgentConfig{
			Effort: DefaultEffort, SubagentInvestigateEffort: DefaultSubagentInvestigate, SubagentChangeEffort: DefaultSubagentChange,
			SubagentConnectorDevelopEffort: DefaultSubagentConnectorDevelop,
			CompactionTokens:               DefaultCompactionTokens, ThinkingPrefixMismatch: DefaultThinkingPrefixMismatch,
			TaskTurns: DefaultTaskTurns, PlatformTurns: DefaultPlatformTurns, ClosestExamplePick: DefaultClosestExamplePick,
		},
		Sandbox: SandboxConfig{Hold: DefaultSandboxHold, PreviewHold: DefaultSandboxPreviewHold, Profile: DefaultSandboxProfile},
		Attachments: AttachmentsConfig{
			MaxPerMessage: DefaultAttachmentsPerMessage, ImageLongEdge: DefaultAttachmentImageLongEdge, ImageMaxBytes: DefaultAttachmentImageMaxBytes,
			DocumentMaxBytes: DefaultAttachmentDocumentMaxBytes, JSONMaxBytes: DefaultAttachmentJSONMaxBytes, MessageInlineText: DefaultAttachmentMessageInlineText,
			SegmentBytes: DefaultAttachmentSegmentBytes, SegmentImages: DefaultAttachmentSegmentImages,
			UploadWait: DefaultAttachmentUploadWait, StagingRetention: DefaultAttachmentStagingRetention,
			TeamSpaceStagedFiles: DefaultAttachmentTeamSpaceStagedFiles, TeamSpaceStagedBytes: DefaultAttachmentTeamSpaceStagedBytes,
			ProjectLifetimeFiles: DefaultAttachmentProjectLifetimeFiles, ProjectLifetimeBytes: DefaultAttachmentProjectLifetimeBytes,
			RenditionCacheBytes: DefaultAttachmentRenditionCacheBytes,
		},
		Live:         LiveConfig{RetirementDrain: DefaultLiveRetirementDrain},
		Builds:       BuildsConfig{LocalURL: DefaultLocalBuildURL, LogReadsPerSecond: DefaultBuildLogReadsPerSecond},
		GitHub:       GitHubConfig{Owner: DefaultGitHubOwner},
		ProjectCloud: ProjectCloudConfig{TemporalRetentionDays: DefaultTemporalRetentionDays, TemporalAddress: DefaultTemporalAddress},
		Routes:       RoutesConfig{LocalPort: DefaultLocalRoutePort},
		Postgres:     PostgresConfig{Port: DefaultPostgresPort, Database: DefaultPostgresDatabase, SSLMode: DefaultPostgresSSLMode, MaxConnections: DefaultPostgresMaxConnections},
		Connections: ConnectionsConfig{ReadListen: DefaultConnectionsReadListen, SlackRelay: SlackRelayConfig{
			ListenAddress: DefaultSlackRelayListen,
			Queue: SlackRelayQueueConfig{
				Implementation: DefaultSlackRelayQueue, MaxEvents: DefaultSlackRelayMaxEvents, MaxBytes: DefaultSlackRelayMaxBytes,
				MaxEventBytes: DefaultSlackRelayMaxEventBytes, MaxScopeEvents: DefaultSlackRelayMaxScopeEvents,
			},
			Delivery: SlackRelayDelivery{Timeout: DefaultSlackRelayTimeout, Concurrency: DefaultSlackRelayConcurrency},
		}},
		RemoteAgent: RemoteAgentConfig{Listen: DefaultRemoteAgentListen, MaxLongPolls: DefaultRemoteAgentMaxLongPolls, MaxConcurrentReads: DefaultRemoteAgentMaxConcurrentReads, MaxConcurrentReports: DefaultRemoteAgentMaxConcurrentReports,
			MinimumBridgeVersion: DefaultMinimumBridgeVersion, DownloadsDir: DefaultBridgeDownloadsDir},
	}
}

// Load applies the defaults and then each YAML file in order, so a later file
// overrides the keys it sets in an earlier one. Parsing is strict: an unknown
// key is an error. Load does not validate; call Validate on the result.
func Load(paths ...string) (Config, error) {
	config := Default()
	for _, path := range paths {
		if err := decodeFile(&config, path); err != nil {
			return Config{}, fmt.Errorf("configuration file %s: %w", path, err)
		}
	}
	return config, nil
}

func decodeFile(config *Config, path string) error {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	// A file with no keys changes nothing.
	if err = decoder.Decode(config); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
