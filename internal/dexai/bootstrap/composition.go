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
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	serverconfig "github.com/superdurable/superversev2/cmd/superverse-server/config"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/attachments"
	"github.com/superdurable/superversev2/internal/dexai/agents/diagnostics"
	"github.com/superdurable/superversev2/internal/dexai/agents/platformissue"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/backgroundcommand"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel/summarymodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/api"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/tenancy"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deliverydao"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/repobinding"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/studio/runs"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/slackrelay"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// composition is the assembled server that serve runs.
type composition struct {
	dex      *dex.Client
	worker   *dex.Worker
	business *grpc.Server
	// routes serve the gateway the public routes of the Docker runtime; nil
	// with Kubernetes, whose Ingress controller reads them itself.
	routes http.Handler
	// credentials serve the applications' credential reads and Slack
	// trigger registrations.
	credentials http.Handler
	remoteAgent *remoteagent.Endpoint
	// slack relays the platform Slack application's events.
	slack *slackrelay.Relay
}

// compose builds the providers, then each domain's Flows, Flow clients and
// Services, and finally binds them to the one Dex client. Everything it opens
// is released through owned, also when composition fails.
func compose(ctx context.Context, config serverConfig, logger *slog.Logger, owned *resources) (*composition, error) {
	routes := config.routes
	newProviders := newKubernetesProviders
	if config.docker != nil {
		newProviders = newDockerProviders
	}
	providers, err := newProviders(ctx, config, owned)
	if err != nil {
		return nil, err
	}

	db, err := postgres.Open(ctx, config.postgres)
	if err != nil {
		return nil, err
	}
	owned.add(db.Close)
	memberDAO := memberdao.New(db)
	accountService := &accounts.Accounts{DAO: accountdao.New(db, config.platformAdminEmails), Signer: config.signer, LoginHost: config.loginHost}
	// One provider client with the address policy makes every provider call
	// of the TeamSpace connections.
	providerClient := connections.NewProviderHTTPClient(config.privateProviderHTTP)
	owned.add(func() error { providerClient.CloseIdleConnections(); return nil })
	connectorReleases := connectormanifest.NewReleaseHTTPClient()
	owned.add(func() error { connectorReleases.CloseIdleConnections(); return nil })
	// The replica's one copy of the published connector catalog, kept warm until the server stops,
	// serves the TeamSpace connections and the catalog matches of each Session's first request.
	publishedCatalog := connectorlibrary.NewPublishedCatalog(connectorReleases)
	refreshing, stopRefresh := context.WithCancel(ctx)
	refreshed := make(chan struct{})
	go func() { publishedCatalog.Run(refreshing, logger); close(refreshed) }()
	owned.add(func() error { stopRefresh(); <-refreshed; return nil })
	connectionDAO, releaseResolver := connectiondao.New(db), connectormanifest.NewReleaseResolver(connectorReleases, config.connectors.ReleaseCandidateDownloadURL())
	credentialReader := connections.NewCredentialReader(connectionDAO, config.objects, releaseResolver, providerClient, config.platformClients, logger)
	shared := confighost.SharedConnections{
		Connections: connections.NewTeamSpaceConnections(connectionDAO, config.objects, releaseResolver, connectormanifest.NewCatalog(connectorReleases, publishedCatalog),
			config.platformClients, providerClient, credentialReader, config.formKey, config.privateProviderHTTP),
		FormKey: config.formKey, Reader: credentialReader, Usages: connections.NewUsages(connectionDAO, config.objects), ReadURL: config.credentialReadURL, Providers: providerClient, PlainHTTP: config.privateProviderHTTP,
	}

	// ProjectFlow gets its dependencies with the back references below.
	projectFlow := &projects.ProjectFlow{}
	teamSpaceFlow := &teamspaces.TeamSpaceFlow{}
	teamSpaceClient := &teamspaces.TeamSpaceFlowClient{Flow: teamSpaceFlow}
	projectClient := &projects.ProjectFlowClient{Flow: projectFlow, Projections: projectdao.New(db), Members: memberDAO, ProjectIDLength: config.projectIDLength, PlatformAdminEmails: config.platformAdminEmails}
	deliveryRows := deliverydao.New(db)
	repositoryFlow := &repobinding.RepositoryBindingFlow{}
	repositoryClient := &repobinding.RepositoryBindingFlowClient{Flow: repositoryFlow}
	shared.ProjectTeamSpace = func(ctx context.Context, projectID string) (string, error) {
		project, err := projectClient.Read(ctx, projectID)
		return project.TeamSpaceID, err
	}
	configuration := &projectconfig.Configuration{Projects: projectClient, Logger: logger, Objects: config.objects, Storage: providers.storage, Shared: shared,
		ReleaseCandidates: config.connectors.ReleaseCandidateDownloadURL()}
	owned.add(func() error { configuration.Close(); return nil })
	setupFlow := &setup.ProjectSetupFlow{Client: projectClient, Repositories: repositoryClient, Host: config.host, Objects: config.objects, Storage: providers.storage, Redactor: providers.redactor, Configurations: configuration}
	setupClient := &setup.ProjectSetupFlowClient{Flow: setupFlow}

	sessionService := &agents.SessionFlowService{Projects: projectClient, Logger: logger}
	// The Agent's platform tools read Flows built below; see the back references.
	projectDiagnostics := &diagnostics.ProjectDiagnostics{}
	// The mock model runs in this process on a loopback port; the Sessions on
	// it send their ordinary Claude HTTP requests there with its placeholder key.
	if config.mockModel {
		mock, stop, startErr := mockmodel.StartBuiltin(ctx, logger, config.providers.CustomEndpoints, config.providers.MockListen)
		if startErr != nil {
			return nil, startErr
		}
		owned.add(func() error { stop(); return nil })
		mock.CompatibleTransports = config.localKind
		config.agent.Mock = mock
	}
	mockURL := ""
	if config.agent.Mock != nil {
		mockURL = config.agent.Mock.BaseURL
	}
	compatible, err := modelprovider.NewCompatibleProviders(ctx, config.providers, config.modelEndpoints, mockURL, logger, config.agent.Claude.BaseURL)
	if err != nil {
		return nil, err
	}
	config.agent.Compatible = compatible
	owned.add(compatible.Close)
	// The Session Agent Flow bootstrap owns the one pooled Sandbox resolver and
	// the instance provider on it; Session tools, the workspace Flow's
	// reclamation, the provisioning, the Preview and the deleting Project's
	// release share them.
	// The Session's model calls render attachment references from the artifact storage, binding each
	// attachment to its Project on first use; the replica caches the rendered blocks.
	renditions := attachments.NewRenditionStore(config.objects, config.renditionCacheBytes)
	// The replica's one extractor runs the server binary as its child, one file at a time, for every
	// AttachmentFlow this replica's Worker prepares.
	attachmentFlows := attachments.NewAttachmentFlowBootstrap(config.objects, fileextract.NewExtractor(config.extractor), teamSpaceClient, config.attachments)
	sessionAgent, err := sessionagent.NewSessionAgentFlowBootstrap(config.agent, &git.HostCredentials{Host: config.host}, sessionService, projectDiagnostics, publishedCatalog, renditions,
		providers.sandboxAddresses, providers.instances, logger)
	if err != nil {
		return nil, err
	}
	owned.add(sessionAgent.Close)
	// A subagent shares the Session's pooled Sandbox resolver and runs on the
	// model its Session ran on.
	subagents, err := subagent.NewSubagentFlowBootstrap(subagent.SubagentFlowConfig{Compatible: config.agent.Compatible, Claude: config.agent.Claude, Mock: config.agent.Mock, Settings: config.agent.Settings,
		Efforts: config.subagentEfforts}, sessionService, projectDiagnostics, sessionAgent.Workspace(), logger)
	if err != nil {
		return nil, err
	}
	owned.add(subagents.Close)
	// A contribution reports to its Session through the Service and opens its
	// pull request in the connectors library.
	contributions := contribution.NewConnectorContributionFlowBootstrap(sessionService, config.connectors, config.connectors, config.connectors, projectDiagnostics)
	// A Session's background command is watched by a SubFlow on the same pooled Sandbox resolver.
	backgroundCommands := backgroundcommand.NewBackgroundCommandFlowBootstrap(sessionAgent.Workspace())
	if config.mockLLM {
		endpoint, parseErr := url.Parse(config.credentialReadURL)
		if parseErr != nil {
			return nil, parseErr
		}
		endpoint.Path = summarymodel.Path
		providers.grants = localModelGrants{Grants: providers.grants, endpoint: endpoint.String()}
	}
	previewConfiguration := preview.NewPreviewConfiguration(projectClient, config.host, config.objects, configuration, providers.storage, providers.grants, providers.cleanup)
	issues := platformissue.NewPlatformIssueFlowBootstrap(config.issues, config.environment, logger)
	workspaces := sessionworkspace.NewSessionWorkspaceFlowBootstrap(sessionAgent.Instances(), sessionAgent.Workspace(), sessionService)
	provisioning := provision.NewSandboxProvisionFlowBootstrap(sessionAgent.Instances(), sessionAgent.Workspace(), config.names, sessionService)
	previewFlow := &preview.PreviewFlow{Provider: sessionAgent.Instances(), Workspace: sessionAgent.Workspace(), Names: config.names, Routes: routes,
		Dependency: sessionService, Preview: previewConfiguration}
	previewClient := &preview.PreviewFlowClient{Flow: previewFlow}
	sessionService.Previews, sessionService.Instances, sessionService.PreviewData = previewClient, sessionAgent.Instances(), previewConfiguration

	// Goal measurement reads Live runs through the Project diagnostics, set below, and tells the Session of reviews.
	evolveFlows := evolve.NewEvolveFlowBootstrap(config.evolve, projectClient, config.host, config.objects, evolveDependency{diagnostics: projectDiagnostics, sessions: sessionService})
	evolveClient := evolveFlows.Client()
	sessionService.Evolve = evolveClient
	deploymentClient := deployments.NewProjectDeployFlowClient(projectClient, providers.serving, providers.grants)
	publishingService := deployments.NewPublishingFlowService(projectClient, deploymentClient, configuration, shared.Usages, deployments.BuildLogReads{Redactor: providers.redactor, PerSecond: config.buildLogReadsPerSecond})
	// Both delivery Flows report their failures to the Project's Session.
	releaseFlow := releases.NewProjectReleaseFlow(projectClient, config.host, config.builder, config.profiles, config.objects, config.environment, publishingService, sessionService)
	releaseClient := releases.NewProjectReleaseFlowClient(releaseFlow, projectClient, config.profiles)
	deploymentFlow := deployments.NewProjectDeployFlow(projectClient, releaseClient, deploymentClient, config.liveRetirementDrain, config.objects, configuration, shared.Usages, providers.serving, providers.storage, providers.execution, providers.grants, providers.redactor, sessionService, evolveClient)

	// Back references. Each field below refers to a component built later that
	// itself depends on the field's owner, so it can only be set now.
	teamSpaceFlow.Projects, teamSpaceFlow.Connections, teamSpaceFlow.Objects, teamSpaceFlow.Staging = projectClient, shared.Usages, config.objects, config.staging
	*projectFlow = projects.ProjectFlow{Client: projectClient, TeamSpaces: teamSpaceClient, Setups: setupClient, Repositories: repositoryClient, Host: config.host, Sessions: sessionService, Releases: releaseClient, Deployments: deploymentClient, Evolve: evolveClient, Objects: config.objects, Storage: providers.storage, Cleanup: providers.cleanup,
		Connections: shared.Usages, Attachments: config.projectAttachments}
	configuration.Preview = sessionService
	configuration.Releases = releaseClient
	deploymentClient.Releases = releaseClient
	releaseClient.Rows, deploymentClient.Rows = deliveryRows, deliveryRows
	releaseClient.Previews = sessionService

	// Second phase: the one Dex client needs the complete registry, so the
	// clients built above are bound to it before the Worker or any listener
	// starts. Release and Deployment clients reach Dex through projectClient.
	flows := serverFlows{
		teamSpace: teamSpaceFlow, project: projectFlow, setup: setupFlow, repository: repositoryFlow,
		workspace: workspaces.Flow(), provision: provisioning.Flow(), preview: previewFlow, session: sessionAgent.Flow(), subagent: subagents.Flow(), background: backgroundCommands.Flow(), issue: issues.Flow(), contribution: contributions.Flow(), release: releaseFlow, deployment: deploymentFlow,
		attachment: attachmentFlows.Flow(), evolve: evolveFlows.Flow(),
	}.list()
	registry, err := dex.NewRegistry(flows)
	if err != nil {
		return nil, err
	}
	if pull := config.dexWorker.Pull; pull != nil {
		// SDK 1.14.1 retires surplus idle streams after busy tasks finish.
		streams := pull.MaxConcurrentTasks + (len(flows)+1)*pull.IdleStreamsPerQueue
		if streams > serverconfig.PlatformDexWorkerStreamCap {
			return nil, fmt.Errorf("platform pull Worker needs up to %d streams for %d Flow types; matching cap is %d: review concurrency and matching capacity", streams, len(flows), serverconfig.PlatformDexWorkerStreamCap)
		}
	}
	cache, err := blobcache.New(&config.blobCache)
	if err != nil {
		return nil, err
	}
	owned.add(cache.Close)
	worker, err := dex.NewWorker(registry, cache, config.dexWorker)
	if err != nil {
		return nil, err
	}
	target := worker.WorkerTarget()
	config.dexClient.WorkerTarget = target
	client, err := dex.NewClient(registry, cache, config.dexClient)
	if err != nil {
		return nil, err
	}
	owned.add(client.Close)
	projectClient.Dex = client
	teamSpaceClient.Dex = client
	setupClient.Dex = client
	repositoryClient.Dex = client
	previewClient.Dex = client
	evolveClient.Bind(client)
	executions, err := grpc.NewClient(config.dexClient.FlowServiceAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	owned.add(executions.Close)
	previewClient.Execution = dexpb.NewFlowServiceClient(executions)
	sessionService.Provisions = provisioning.Client(client, dexpb.NewFlowServiceClient(executions))
	sessionService.Workspaces = workspaces.Client(client, dexpb.NewFlowServiceClient(executions))
	sessionAgentClient := sessionAgent.Client(client, dexpb.NewFlowServiceClient(executions))
	sessionService.Agent, sessionService.Subagents, sessionService.Issues, sessionService.Contributions = sessionAgentClient, subagents.Client(client, dexpb.NewFlowServiceClient(executions)), issues.Client(client), contributions.Client(client)
	sessionService.ModelCredentials = &agents.ModelCredentialFlowService{Sessions: sessionAgentClient, Subagents: sessionService.Subagents, Compatible: config.agent.Compatible, Logger: logger}
	sessionService.BackgroundCommands = backgroundCommands.Client(client)

	flowRuns := &runs.Runs{
		Projects: projectClient, Deployments: deploymentClient, Sessions: sessionService,
		Objects: config.objects, Connections: providers.connections, Names: config.names, Workspaces: sessionAgent.Workspace(), Logger: logger,
		DexConnections: config.runsDexConnections,
	}
	owned.add(flowRuns.Close)
	*projectDiagnostics = diagnostics.ProjectDiagnostics{
		Projects: projectClient, Previews: previewClient, Releases: releaseClient, Deployments: deploymentClient,
		Configuration: configuration, Runs: flowRuns, Platform: dexpb.NewFlowServiceClient(executions), Serving: providers.serving, Redactor: providers.redactor,
		Publishing:    publishingService,
		Contributions: sessionService.Contributions, ConnectorChecks: config.connectors, Attachments: renditions,
	}
	slack := newSlackRelay(config, connectionDAO, configuration, logger)
	remoteDAO := remoteagentdao.New(db)
	sessionService.RemoteConnections = remoteDAO
	endpoint := config.studioOrigin
	if config.docker != nil {
		endpoint = "http://" + config.remoteAgent.Listen
	} else if config.localKind {
		endpoint = "http://127.0.0.1:" + strconv.Itoa(serverconfig.LocalKindRemoteAgentPort)
	}
	remoteService := &remoteagent.Connections{DAO: remoteDAO, Sessions: sessionService, Endpoint: endpoint,
		MinimumBridgeVersion: config.remoteAgent.MinimumBridgeVersion, AcceptDevBridge: config.remoteAgent.AcceptDevBridge, PlatformAdminEmails: config.platformAdminEmails}
	attachmentClient := attachmentFlows.Client(client)
	var rpcCoverage *api.RPCCoverage
	if config.rpcCoverageFile != "" {
		rpcCoverage, err = api.NewRPCCoverage(config.rpcCoverageFile)
		if err != nil {
			return nil, fmt.Errorf("RPC coverage file: %w", err)
		}
	}
	business := api.NewGRPCServer(&api.Server{
		RPCCoverage:     rpcCoverage,
		Boundary:        &boundary.Boundary{Logger: logger, Accounts: accountService, Dex: client, Cursors: config.cursors},
		ProjectIDLength: config.projectIDLength, Sessions: sessionService, SessionAgents: sessionAgentClient, Subagents: sessionService.Subagents,
		Projects: projectClient, TeamSpaces: teamSpaceClient, TeamSpaceReads: teamspacedao.New(db), Members: memberDAO, Setups: setupClient, Host: config.host, Configuration: configuration, Runs: flowRuns,
		Releases: releaseClient, Deployments: deploymentClient, Publishing: publishingService, RecentEvents: slack,
		Attachments: attachmentClient, Renditions: renditions, AttachmentAdmission: attachments.NewAttachmentFlowService(attachmentClient, projectClient, config.attachmentsPerMessage),
		Connections: tenancy.Connections{Connections: shared.Connections, Access: connectionDAO, StudioOrigin: config.oauthCallbackOrigin, Unrouted: slack},
		// Claude credentials belong to each Project; mock availability is configured.
		SessionModels:      api.SessionModels{Claude: true, Mock: config.mockModel, Local: config.localKind, Providers: config.providers},
		ClosestExamplePick: config.closestExamplePick,
		RemoteAgents:       remoteService,
		Evolve:             evolveClient,
		EvolveSettings:     config.evolve,
	})
	credentials := http.NewServeMux()
	credentials.Handle(connections.CredentialReadPath, connections.NewReadEndpoint(credentialReader))
	credentials.HandleFunc(slackrelay.RegistrationPath, slack.ServeRegistration)
	if config.mockLLM {
		credentials.Handle(summarymodel.Path, summarymodel.New(logger))
	}
	remoteEndpoint := remoteagent.NewEndpoint(remoteService, config.remoteAgent.DownloadsDir, logger, config.remoteAgent.MaxLongPolls, config.remoteAgent.MaxConcurrentReads, config.remoteAgent.MaxConcurrentReports)
	owned.add(func() error { remoteEndpoint.Close(); return nil })
	return &composition{dex: client, worker: worker, business: business, routes: providers.routes, credentials: credentials, slack: slack,
		remoteAgent: remoteEndpoint}, nil
}

// localModelGrants injects a reserved platform setting, outside application configuration.
type localModelGrants struct {
	infra.Grants
	endpoint string
}

func (g localModelGrants) GrantEnvironment(ctx context.Context, workload infra.Workload) (map[string]string, error) {
	environment, err := g.Grants.GrantEnvironment(ctx, workload)
	if err != nil {
		return nil, err
	}
	environment["SUPERVERSE_LOCAL_MOCK_LLM_URL"] = g.endpoint
	return environment, nil
}

// evolveDependency serves EvolveFlow: the Live reads of the Project
// diagnostics and the Session notifications of the Session Service.
type evolveDependency struct {
	diagnostics *diagnostics.ProjectDiagnostics
	sessions    *agents.SessionFlowService
}

var _ evolve.EvolveFlowDependency = evolveDependency{}

func (d evolveDependency) InvokeEvolveLive(ctx context.Context, projectID string, read func(context.Context, dexpb.FlowServiceClient) error) error {
	return d.diagnostics.InvokeEvolveLive(ctx, projectID, read)
}

func (d evolveDependency) CurrentLive(ctx context.Context, projectID string) (evolve.LiveRelease, error) {
	return d.diagnostics.CurrentLive(ctx, projectID)
}

func (d evolveDependency) LiveRedaction(ctx context.Context, projectID string) (func(string) string, error) {
	return d.diagnostics.LiveRedaction(ctx, projectID)
}

func (d evolveDependency) LiveTimeline(ctx context.Context, projectID string, until time.Time) ([]runexport.Release, error) {
	return d.diagnostics.LiveTimeline(ctx, projectID, until)
}

func (d evolveDependency) ReportEvolveNotification(ctx context.Context, projectID string, notice evolve.EvolveNotice) error {
	return d.sessions.ReportEvolveNotification(ctx, projectID, notice)
}
