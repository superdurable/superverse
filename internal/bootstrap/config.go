// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/agents/platformissue"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/api/watch"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"
	buildhosted "github.com/superdurable/superversev2/internal/delivery/releases/build/hosted"
	buildlocal "github.com/superdurable/superversev2/internal/delivery/releases/build/local"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/githubapp"
	"github.com/superdurable/superversev2/internal/platform/githubissues"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/platform/repohost"
	"github.com/superdurable/superversev2/internal/platform/repohost/github"
	"github.com/superdurable/superversev2/internal/platform/repohost/localgit"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/projects/infra/docker"
	infrahosted "github.com/superdurable/superversev2/internal/projects/infra/kube/hosted"
	infralocal "github.com/superdurable/superversev2/internal/projects/infra/kube/local"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// serverConfig is what composition needs to start: the validated operator
// configuration and the secrets from the environment, together with the
// providers that need nothing else. The settings are read and validated by
// cmd/superverse-server/config; the secrets only here (see secrets.go).
type serverConfig struct {
	environment deploymentnames.Environment
	names       deploymentnames.Names
	localKind   bool
	aws         aws.Config

	// signer issues login tokens; cursors signs the API's resume cursors and
	// page tokens with the same key.
	signer  *accounts.TokenSigner
	cursors *watch.CursorCodec
	// formKey is the root key of the connection forms' nonces, the same in
	// every replica so that each opens the others'.
	formKey            []byte
	projectIDLength    int
	platformAdminEmail string
	// runsDexConnections bounds the Dex connections of Studio Runs.
	runsDexConnections int
	studioOrigin       string
	// oauthCallbackOrigin is the origin of the connections' OAuth redirect
	// URI: Studio's, unless a local manual test names a tunnel host.
	oauthCallbackOrigin string
	agent               sessionagent.SessionAgentConfig
	// subagentEfforts are the base efforts of the subagent kinds.
	subagentEfforts map[agenttasks.SubagentKind]conversation.Effort
	// mockModel starts the mock model in this process, which composition
	// offers to every Session besides the Claude models.
	mockModel bool

	sandboxProfileName string
	// docker is the Docker runtime of Local Native, with the rendered coding
	// profile at sandboxProfilePath; it is nil with runtime kubernetes.
	docker             *infradocker.Config
	sandboxProfilePath string
	// nativeRunID labels the health answers of a Local Native server, whose
	// launcher accepts readiness only from the run it started, and
	// routesListen is where it serves the gateway its routes.
	nativeRunID, routesListen string
	// routes are the public application routes. The Kubernetes providers get
	// their cluster connections in composition, which builds the project cloud
	// account from hostedProjects outside the local lanes.
	routes         infra.PublicRoutes
	hostedProjects infrahosted.Config
	// liveDexImage runs each project's Dex; localImages run the MinIO and the
	// Temporal of a Local Kind project.
	liveDexImage string
	localImages  infralocal.Backend
	// liveRetirementDrain is how long a replaced Live generation keeps running.
	liveRetirementDrain time.Duration
	// buildLogReadsPerSecond bounds the build log reads of this replica.
	buildLogReadsPerSecond float64

	// postgres is the database of the accounts, as the server's role.
	postgres postgres.Connection
	// credentialRead is where serve opens the credential read endpoint of
	// the TeamSpace connections; privateProviderHTTP lets provider calls use
	// plain http to loopback and private addresses, in the local lanes only;
	// platformClients are the platform's OAuth applications.
	credentialRead, credentialReadURL string
	remoteAgent                       serverconfig.RemoteAgentConfig
	privateProviderHTTP               bool
	platformClients                   connections.PlatformClients
	// slackRelay relays the platform Slack application's events, with its
	// Signing Secret; without one its events listener does not serve.
	slackRelay         serverconfig.SlackRelayConfig
	slackSigningSecret []byte

	host repohost.Host
	// localGit is the local Git host when host is one, which serve also opens on
	// gitAddress; nil when the host is GitHub.
	localGit   *localgit.Host
	gitAddress string
	objects    *objectstore.Store
	profiles   *build.Profiles
	// builder is nil in Local Kind without a build token, which deliberately
	// leaves Release execution unavailable.
	builder build.Builder
	// issues is where PlatformIssueFlow files platform faults: the issue
	// repository's installation of the GitHub App, or NotConfigured.
	issues platformissue.PlatformIssueFlowDependency
	// connectors is the connectors library the develop subagents contribute
	// to and their contributions open pull requests in.
	connectors *connectorlibrary.Library

	dexClient                  dex.ClientOptions
	dexWorker                  dex.WorkerOptions
	blobCache                  blobcache.Config
	grpcAddress, healthAddress string
}

// loadConfig turns the validated settings and the secrets of the environment
// into the server's providers, once, before anything starts. Settings that
// only one mode uses are used only in that mode: the local build service in
// Local Kind, CodeBuild and the project cloud elsewhere.
func loadConfig(ctx context.Context, logger *slog.Logger, settings serverconfig.Config) (c serverConfig, err error) {
	// Validate accepted the name, so parsing it cannot fail.
	c.environment = comm.Must(deploymentnames.ParseEnvironment(settings.Environment))
	c.names = deploymentnames.MustFor(c.environment)
	c.localKind = settings.LocalKind
	if c.aws, err = awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(settings.AWS.Region)); err != nil {
		return c, fmt.Errorf("AWS configuration: %w", err)
	}

	key, err := cookieSigningKey()
	if err != nil {
		return c, err
	}
	if c.signer, err = accounts.NewTokenSigner(key); err != nil {
		return c, err
	}
	c.cursors = watch.NewCursorCodec(key)
	c.formKey = key
	c.projectIDLength, c.runsDexConnections = settings.Server.ProjectIDLength, settings.Studio.RunsDexConnections
	c.platformAdminEmail = settings.Accounts.PlatformAdminEmail
	c.studioOrigin, c.oauthCallbackOrigin = settings.Studio.PublicOrigin, settings.Connections.OAuthCallbackOrigin
	if c.oauthCallbackOrigin == "" {
		c.oauthCallbackOrigin = c.studioOrigin
	}
	c.mockModel = settings.Agent.MockModel
	c.subagentEfforts = map[agenttasks.SubagentKind]conversation.Effort{
		agenttasks.SubagentInvestigate: settings.Agent.SubagentInvestigateEffort, agenttasks.SubagentChange: settings.Agent.SubagentChangeEffort,
		agenttasks.SubagentConnectorSelect:  agenttasks.SubagentConnectorSelect.DefaultEffort(),
		agenttasks.SubagentConnectorDevelop: settings.Agent.SubagentConnectorDevelopEffort,
	}
	if c.agent, err = agentConfig(settings.Agent, settings.Sandbox); err != nil {
		return c, err
	}

	c.sandboxProfileName = settings.Sandbox.Profile
	c.liveDexImage = settings.Live.DexImage
	c.liveRetirementDrain = settings.Live.RetirementDrain
	c.buildLogReadsPerSecond = settings.Builds.LogReadsPerSecond
	c.localImages = infralocal.Backend{S3Image: settings.Live.S3Image, TemporalImage: settings.Live.TemporalImage}

	if err = c.repositoryHost(settings, logger); err != nil {
		return c, err
	}
	if c.issues, err = platformIssues(settings.GitHub); err != nil {
		return c, err
	}
	if c.connectors, err = connectorLibrary(settings, repositoryPrefix(settings, c.environment), c.localGit); err != nil {
		return c, err
	}
	c.agent.ConnectorLibrary = c.connectors
	if c.localGit != nil {
		// A branch of the local Git host's copy resolves for go get in the Sandbox.
		c.agent.ModuleRedirects = []sandboxd.ModuleRedirect{{ModulePath: connectorlibrary.ModulePath, CloneURL: c.localGit.ConnectorLibraryCloneURL()}}
	}
	if c.postgres, err = postgresConnection(settings.Postgres, postgres.ServerRole); err != nil {
		return c, err
	}
	if c.objects, err = c.objectStore(settings.Storage); err != nil {
		return c, err
	}
	c.credentialRead, c.credentialReadURL = settings.Connections.ReadListen, settings.Connections.ReadURL
	c.remoteAgent = settings.RemoteAgent
	c.privateProviderHTTP = settings.Connections.AllowInsecureOAuthEndpoints
	if c.platformClients, err = platformOAuthClients(settings.Connections.PlatformClients); err != nil {
		return c, err
	}
	c.slackRelay, c.slackSigningSecret = settings.Connections.SlackRelay, slackSigningSecret()
	for key := range settings.Connections.PlatformClients {
		if strings.HasPrefix(key, connections.SlackConnectorID+"/") && len(c.slackSigningSecret) == 0 {
			return c, fmt.Errorf("connections.platformClients has %s, whose events need SUPERVERSE_SLACK_SIGNING_SECRET", key)
		}
	}
	// Local Kind builds Releases on the host with the local build profile and
	// serves projects in its own cluster, every application on localhost
	// through its gateway. Every other
	// environment builds them on CodeBuild and serves projects from the project
	// cloud account, with the hosted default build profile outside prod.
	c.profiles = build.NewProfiles()
	if c.localKind {
		c.routes = infra.PublicRoutes{
			Environment: c.environment, Local: true, LocalDomain: "localhost",
			LocalPort: settings.Routes.LocalPort, LocalGatewayURL: settings.Routes.GatewayURL,
		}
		c.profiles.SelectLocalDefault()
		if c.builder, err = localBuilder(settings.Builds.LocalURL); err != nil {
			return c, err
		}
		if settings.Runtime == serverconfig.RuntimeDocker {
			c.nativeDocker(settings)
		}
	} else {
		c.hostedProjects = hostedProjects(settings, c.environment, c.aws.Region)
		c.routes = infra.PublicRoutes{Environment: c.environment}
		if c.environment != deploymentnames.EnvironmentProd {
			c.profiles.SelectHostedDefault()
		}
		if c.builder, err = hostedBuilder(c.aws, settings, c.environment, c.profiles); err != nil {
			return c, err
		}
	}

	// The Dex Server reaches this process's Worker at target.
	target := dex.WorkerTarget{Address: settings.Dex.WorkerTarget, Headless: settings.Dex.WorkerHeadless}
	c.dexClient = dex.ClientOptions{FlowServiceAddress: settings.Dex.ServerAddress, WorkerTarget: &target}
	c.dexWorker = dex.WorkerOptions{BindAddress: settings.Dex.WorkerAddress, WorkerTarget: target, FlowServiceAddress: settings.Dex.ServerAddress}
	c.blobCache = blobcache.Config{Dir: settings.Server.BlobCacheDir, MaxBytes: 256 << 20}
	c.grpcAddress = settings.Server.GRPCAddress
	c.healthAddress = settings.Server.HealthAddress
	return c, nil
}

// nativeDocker configures the Docker runtime of Local Native from validated
// settings: the engine, the project images, the lane's state directory with the
// rendered coding profile, the network prefix and the gateway.
func (c *serverConfig) nativeDocker(settings serverconfig.Config) {
	native := settings.Native
	c.docker = &infradocker.Config{
		Socket: native.DockerSocket, Environment: c.environment, DexImage: settings.Live.DexImage, S3Image: settings.Live.S3Image, TemporalImage: settings.Live.TemporalImage,
		StateDir: native.StateDir, Instance: native.Instance, NetworkPrefix: comm.Must(netip.ParsePrefix(native.NetworkPrefix)),
		Routes: c.routes, GatewayURL: settings.Routes.GatewayURL, GatewayAPI: native.GatewayAPI,
	}
	c.sandboxProfilePath = filepath.Join(native.StateDir, nativeProfilesDirectory, settings.Sandbox.Profile+".yaml")
	c.nativeRunID, c.routesListen = native.RunID, native.RoutesListen
}

// postgresConnection connects to the configured database as role.
func postgresConnection(settings serverconfig.PostgresConfig, role string) (postgres.Connection, error) {
	password, err := postgresPassword(role)
	return postgres.Connection{
		Host: settings.Host, Port: settings.Port, Database: settings.Database, User: role, Password: password,
		SSLMode: string(settings.SSLMode), MaxConnections: settings.MaxConnections,
	}, err
}

// nativeProfilesDirectory holds the coding profiles that the Native launcher
// renders below the lane's state directory.
const nativeProfilesDirectory = "sandbox-profiles"

// objectStore keeps release artifacts in the artifact bucket. Only Kind may
// point S3 at its own endpoint, which the configuration enforces.
func (c *serverConfig) objectStore(storage serverconfig.StorageConfig) (*objectstore.Store, error) {
	client := s3.NewFromConfig(c.aws, func(o *s3.Options) {
		if storage.S3Endpoint != "" {
			o.BaseEndpoint = aws.String(storage.S3Endpoint)
			o.UsePathStyle = true
		}
	})
	return objectstore.NewStore(client, storage.ArtifactBucket, storage.ArtifactKMSKeyID, c.names.WorkloadPrefix+"-"+c.environment.String(), c.localKind)
}

// localBuilder builds Releases on the host build service. Without a token,
// preferably read from its file, Local Kind has no builder.
func localBuilder(url string) (build.Builder, error) {
	token, err := localBuildToken()
	if err != nil || token == "" {
		return nil, err
	}
	return buildlocal.New(url, token)
}

// hostedProjects assembles the project cloud account settings.
func hostedProjects(settings serverconfig.Config, environment deploymentnames.Environment, region string) infrahosted.Config {
	cloud := settings.ProjectCloud
	return infrahosted.Config{
		AccountID: settings.AWS.AccountID, Region: region, ClusterName: cloud.EKSClusterName, Environment: environment,
		BucketPrefix:           cloud.BucketPrefix,
		KMSKeyARN:              cloud.KMSKeyARN,
		PermissionsBoundaryARN: cloud.PermissionsBoundaryARN,
		TemporalAccountID:      cloud.TemporalAccountID,
		TemporalRegion:         cloud.TemporalRegion,
		TemporalAPIKey:         projectTemporalAPIKey(),
		TemporalRetentionDays:  cloud.TemporalRetentionDays,
		TemporalKeyRenewWindow: time.Duration(cloud.TemporalKeyRenewDays) * 24 * time.Hour,
	}
}

// hostedBuilder builds Releases on CodeBuild in the project cloud account
// with the given build profiles.
func hostedBuilder(cloud aws.Config, settings serverconfig.Config, environment deploymentnames.Environment, profiles *build.Profiles) (build.Builder, error) {
	hostedBuild := settings.Builds.Hosted
	return buildhosted.New(cloud, buildhosted.Config{
		Environment: environment, ProjectName: hostedBuild.Project, Region: cloud.Region, AccountID: settings.AWS.AccountID,
		RepositoryURI: hostedBuild.Repository,
		Bucket:        hostedBuild.Bucket,
		Prefix:        hostedBuild.Prefix,
		KMSKeyARN:     hostedBuild.KMSKeyARN,
		ToolImage:     hostedBuild.ToolImage,
		HostImage:     hostedBuild.HostImage,
	}, profiles)
}

// repositoryPrefix starts the names of the managed repositories, and this
// server's lane alone decides it: Local Native (runtime docker) is
// localnative, Local Kind (localKind on the Kubernetes runtime, whichever Git
// host) is localkind, and any other server is its environment's name: test,
// staging or prod. The two local lanes run as the test environment, so the
// environment alone could not tell their repositories from AWS test's.
// Validate admits runtime docker only with localKind.
func repositoryPrefix(settings serverconfig.Config, environment deploymentnames.Environment) deploymentnames.RepositoryPrefix {
	switch {
	case settings.Runtime == serverconfig.RuntimeDocker:
		return deploymentnames.RepositoryPrefixLocalNative
	case settings.LocalKind:
		return deploymentnames.RepositoryPrefixLocalKind
	default:
		return deploymentnames.HostedRepositoryPrefix(environment)
	}
}

// repositoryHost builds the Git host the configuration selects. The local Git
// host needs no GitHub App, and GitHub needs its private key.
func (c *serverConfig) repositoryHost(settings serverconfig.Config, logger *slog.Logger) error {
	prefix := repositoryPrefix(settings, c.environment)
	// Validate accepted the name, so parsing it cannot fail.
	switch provider := comm.Must(repohost.ParseProvider(settings.RepositoryHost)); provider {
	case repohost.ProviderGitHub:
		var err error
		c.host, err = gitHubHost(settings.GitHub, prefix, c.environment)
		return err
	case repohost.ProviderLocalGit:
		key, err := localGitSigningKey()
		if err != nil {
			return err
		}
		local := settings.LocalGit
		if c.localGit, err = localgit.New(localgit.Config{Dir: local.Dir, CloneBaseURL: local.CloneBaseURL, ServerBaseURL: loopbackURL(local.Listen), SigningKey: key,
			RepositoryPrefix: prefix, Environment: c.environment}, logger); err != nil {
			return err
		}
		c.host, c.gitAddress = c.localGit, local.Listen
		return nil
	default:
		panic("unhandled repository host " + provider.String())
	}
}

// gitHubHost is the GitHub repository host as the App's installation that owns
// the managed repositories.
func gitHubHost(settings serverconfig.GitHubConfig, prefix deploymentnames.RepositoryPrefix, environment deploymentnames.Environment) (repohost.Host, error) {
	app, err := gitHubApp(settings.AppID, settings.InstallationID)
	if err != nil {
		return nil, err
	}
	return &github.Client{App: app, Owner: settings.Owner, RepositoryPrefix: prefix, Environment: environment}, nil
}

// platformIssues is the repository where the platform files its incidents, as
// a second installation of the same GitHub App whose token holds only the
// issue permissions, or NotConfigured, where an incident is only logged, when
// the configuration names none. Validate admits the repository only together
// with its installation.
func platformIssues(settings serverconfig.GitHubConfig) (platformissue.PlatformIssueFlowDependency, error) {
	if settings.Issues.Repository == "" {
		return githubissues.NotConfigured{}, nil
	}
	app, err := gitHubApp(settings.AppID, settings.Issues.InstallationID)
	if err != nil {
		return nil, err
	}
	app.Permissions = githubissues.TokenPermissions()
	return &githubissues.Repository{App: app, FullName: settings.Issues.Repository}, nil
}

// loopbackURL is the HTTP origin at which this process reaches its own listener
// on listen: an unspecified host becomes the loopback address.
func loopbackURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	comm.MustDo(err)
	if host == "" || net.ParseIP(host).IsUnspecified() {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// connectorLibrary is the connectors library that develop subagents contribute
// to: the upstream library that github.connectors names, reached as another
// installation of the same GitHub App whose tokens are minted per operation,
// or none; and in the local lanes the local Git host's copy, which takes the
// contribution branches. Without an upstream no branch reaches GitHub.
func connectorLibrary(settings serverconfig.Config, lane deploymentnames.RepositoryPrefix, local *localgit.Host) (*connectorlibrary.Library, error) {
	var upstream *connectorlibrary.Upstream
	if connectors := settings.GitHub.Connectors; connectors.Repository != "" {
		app, err := gitHubApp(settings.GitHub.AppID, connectors.InstallationID)
		if err != nil {
			return nil, err
		}
		upstream = &connectorlibrary.Upstream{App: app, FullName: connectors.Repository}
	}
	var mirror connectorlibrary.Mirror
	if local != nil {
		mirror = local
	}
	return connectorlibrary.New(upstream, mirror, lane), nil
}

// gitHubApp is one installation of the GitHub App: the IDs come from the
// configuration and the private key from the environment.
func gitHubApp(appID, installationID int64) (*githubapp.App, error) {
	key, err := gitHubPrivateKey()
	if err != nil {
		return nil, err
	}
	privateKey, err := githubapp.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	return &githubapp.App{AppID: appID, InstallationID: installationID, PrivateKey: privateKey}, nil
}

// agentConfig assembles the Session Agent's model settings and its Sandbox
// holds. The Claude API key comes only from ANTHROPIC_API_KEY. Without it the
// server offers no Claude model, which only a server whose new Sessions start
// on the mock model accepts; composition gives the mock model's client its
// fixed placeholder, never a real key.
func agentConfig(settings serverconfig.AgentConfig, sandbox serverconfig.SandboxConfig) (sessionagent.SessionAgentConfig, error) {
	config := sessionagent.SessionAgentConfig{
		Model:          settings.Model,
		Claude:         modelprovider.ClaudeClientConfig{APIKey: strings.TrimSpace(anthropicAPIKey())},
		AutomaticTurns: agentloop.AutomaticTurnBounds{Task: settings.TaskTurns, Platform: settings.PlatformTurns},
		SandboxHold:    sandbox.Hold,
		PreviewHold:    sandbox.PreviewHold,
	}
	if config.Claude.APIKey == "" && settings.Model != conversation.ModelMock {
		return config, errors.New("ANTHROPIC_API_KEY is required: agent.model is a Claude model")
	}
	config.Settings.Effort = settings.Effort
	config.Settings.CompactionTokens = int64(settings.CompactionTokens)
	config.Settings.PrefixMismatch = settings.ThinkingPrefixMismatch
	return config, nil
}
