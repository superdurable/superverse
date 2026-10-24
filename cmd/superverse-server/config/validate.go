// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package config

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

var (
	accountID = regexp.MustCompile(`^[0-9]{12}$`)
	// nativeRunID admits the launcher's run identity, a UUID.
	nativeRunID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{7,63}$`)
	// nativeInstance is the operator-selected name of an isolated Local Native instance.
	nativeInstance = regexp.MustCompile(`^[a-z0-9]{1,12}$`)
)

// loopback is the only host address a Native process listens on and dials
// another Native process at.
const loopback = "127.0.0.1"

// Validate checks every range and every combination of settings, and returns
// all violations at once. A valid Config is what the server composes from.
func (c Config) Validate() error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }

	var names deploymentnames.Names
	environment, err := deploymentnames.ParseEnvironment(c.Environment)
	if err != nil {
		fail("environment: %v", err)
	} else {
		names = deploymentnames.MustFor(environment)
	}
	if c.LocalKind && environment != deploymentnames.EnvironmentTest {
		fail("localKind is restricted to the test environment")
	}
	if len(c.Accounts.PlatformAdminEmails) == 0 || len(c.Accounts.PlatformAdminEmails) > MaxPlatformAdminEmails {
		fail("accounts.platformAdminEmails must contain between 1 and %d addresses", MaxPlatformAdminEmails)
	}
	problems = append(problems, c.validateLoginHost()...)
	seenAdmins := make(map[string]bool, len(c.Accounts.PlatformAdminEmails))
	for _, email := range c.Accounts.PlatformAdminEmails {
		parsed, err := mail.ParseAddress(email)
		if err != nil || parsed.Address != email || strings.ContainsAny(email, " <>\t\r\n/$:") || len(email) > 254 || strings.IndexFunc(email, func(r rune) bool { return r < 33 || r > 127 }) >= 0 || strings.ToLower(email) != email {
			fail("accounts.platformAdminEmails must contain canonical email addresses")
		}
		if seenAdmins[email] {
			fail("accounts.platformAdminEmails must not contain duplicates")
		}
		seenAdmins[email] = true
	}
	if c.Server.RPCCoverageFile != "" && (!c.LocalKind || !filepath.IsAbs(c.Server.RPCCoverageFile)) {
		fail("server.rpcCoverageFile requires a local lane and an absolute path")
	}
	if runtimeErr := c.Runtime.Validate(); runtimeErr != nil {
		fail("runtime: %v", runtimeErr)
	}
	// A key left out of every file keeps its default; a key set to an empty value is an error.
	for _, setting := range []struct{ name, value string }{
		{"aws.region", c.AWS.Region},
		{"server.grpcAddress", c.Server.GRPCAddress}, {"server.healthAddress", c.Server.HealthAddress}, {"server.blobCacheDir", c.Server.BlobCacheDir},
		{"dex.serverAddress", c.Dex.ServerAddress},
		{"sandbox.profile", c.Sandbox.Profile}, {"storage.artifactBucket", c.Storage.ArtifactBucket},
	} {
		if setting.value == "" {
			fail("%s is required", setting.name)
		}
	}

	if !between(c.Dex.Pull.MaxConcurrentTasks, 1, 256) {
		fail("dex.pull.maxConcurrentTasks must be an integer from 1 to 256")
	}
	if c.Dex.Pull.ReservedRPCTasks < 0 || c.Dex.Pull.ReservedRPCTasks >= c.Dex.Pull.MaxConcurrentTasks {
		fail("dex.pull.reservedRPCTasks must be an integer from 0 to maxConcurrentTasks-1")
	}
	if !between(c.Dex.Pull.IdleStreamsPerQueue, 1, 8) {
		fail("dex.pull.idleStreamsPerQueue must be an integer from 1 to 8")
	}
	if c.Dex.Pull.StopDrainTimeout < time.Second || c.Dex.Pull.StopDrainTimeout > 60*time.Second {
		fail("dex.pull.stopDrainTimeout must be from 1s to 60s")
	}
	if c.Postgres.Host == "" || c.Postgres.Database == "" {
		fail("postgres.host and postgres.database are required")
	}
	if !between(c.Postgres.Port, 1, 65535) {
		fail("postgres.port must be an integer from 1 to 65535")
	}
	if !between(c.Postgres.MaxConnections, 1, 100) {
		fail("postgres.maxConnections must be an integer from 1 to 100")
	}
	if sslErr := c.Postgres.SSLMode.Validate(); sslErr != nil {
		fail("postgres.%v", sslErr)
	} else if c.Postgres.SSLMode == PostgresSSLDisable && !c.LocalKind {
		fail("postgres.sslMode %s is restricted to the local lanes", PostgresSSLDisable)
	}
	if err == nil && !between(c.Server.ProjectIDLength, 1, names.MaxProjectIDLength) {
		fail("server.projectIDLength must be an integer from 1 to %d", names.MaxProjectIDLength)
	}
	if !validOrigin(c.Studio.PublicOrigin) {
		fail("studio.publicOrigin must be an http or https origin")
	}
	if !between(c.Studio.RunsDexConnections, 1, 4096) {
		fail("studio.runsDexConnections must be an integer from 1 to 4096")
	}
	problems = append(problems, c.validateConnections()...)
	problems = append(problems, c.validateRemoteAgent()...)
	problems = append(problems, c.validateEvolve()...)

	if modelErr := c.Agent.Model.Validate(); modelErr != nil {
		fail("agent.model: %v", modelErr)
	} else if availability := c.Agent.Providers.Availability(c.Agent.Model); !availability.Selectable() {
		fail("agent.model: %s", availability.Reason())
	}
	if c.Agent.Effort.Validate() != nil {
		fail("agent.effort must be low, medium, high, xhigh or max")
	}
	if c.Agent.SubagentInvestigateEffort.Validate() != nil {
		fail("agent.subagentInvestigateEffort must be low, medium, high, xhigh or max")
	}
	if c.Agent.SubagentChangeEffort.Validate() != nil {
		fail("agent.subagentChangeEffort must be low, medium, high, xhigh or max")
	}
	if c.Agent.SubagentConnectorDevelopEffort.Validate() != nil {
		fail("agent.subagentConnectorDevelopEffort must be low, medium, high, xhigh or max")
	}
	if !between(c.Agent.CompactionTokens, 20_000, 800_000) {
		fail("agent.compactionTokens must be an integer from 20000 to 800000")
	}
	if prefixErr := c.Agent.ThinkingPrefixMismatch.Validate(); prefixErr != nil {
		fail("agent.thinkingPrefixMismatch: %v", prefixErr)
	}
	if !between(c.Agent.TaskTurns, 0, 100) {
		fail("agent.taskTurns must be an integer from 0 to 100")
	}
	if !between(c.Agent.PlatformTurns, 0, 100) {
		fail("agent.platformTurns must be an integer from 0 to 100")
	}
	if err := c.Agent.Providers.Validate(c.LocalKind, c.Agent.MockModel); err != nil {
		fail("%v", err)
	}
	if err := c.Agent.ModelEndpoints.Validate(c.LocalKind); err != nil {
		fail("%v", err)
	}
	if c.Agent.Providers.MockEndpoint && !c.Agent.ModelEndpoints.AllowLocalLoopback {
		fail("agent.providers.mockEndpoint requires agent.modelEndpoints.allowLocalLoopback")
	}
	if c.Agent.MockModel && environment == deploymentnames.EnvironmentProd {
		fail("agent.mockModel is refused in prod")
	}
	// Hosted environments offer the mock model only as the administrator's
	// choice; only Local Kind and Native start new Sessions on it.
	if c.Agent.Model.IsMock() && (!c.Agent.MockModel || !c.LocalKind) {
		fail("agent.model may be mock only with agent.mockModel in Local Kind and Native")
	}

	if !between(c.Sandbox.Hold, time.Minute, 7*24*time.Hour) {
		fail("sandbox.hold must be a duration from 1m0s to 168h0m0s")
	}
	if !between(c.Sandbox.PreviewHold, time.Minute, 7*24*time.Hour) {
		fail("sandbox.previewHold must be a duration from 1m0s to 168h0m0s")
	}
	if !between(c.Live.RetirementDrain, 30*time.Second, 24*time.Hour) {
		fail("live.retirementDrain must be a duration from 30s to 24h0m0s")
	}
	if !between(c.Builds.LogReadsPerSecond, 0.1, 10) {
		fail("builds.logReadsPerSecond must be from 0.1 to 10")
	}

	if c.Storage.S3Endpoint != "" && !c.LocalKind {
		fail("storage.s3Endpoint is restricted to Local Kind")
	}
	// Presigned URLs name the browser endpoint, or the AWS regional endpoint
	// without one, so a storage endpoint of its own needs the browser's.
	if c.Storage.BrowserEndpoint == "" && c.Storage.S3Endpoint != "" {
		fail("storage.browserEndpoint is required with storage.s3Endpoint")
	}
	if browser, parseErr := url.Parse(c.Storage.BrowserEndpoint); c.Storage.BrowserEndpoint != "" && (parseErr != nil || !validOrigin(c.Storage.BrowserEndpoint) || browser.Scheme == "http" && !c.LocalKind) {
		fail("storage.browserEndpoint must be an http or https origin, http only in the local lanes")
	}
	problems = append(problems, c.validateAttachments()...)
	provider, providerErr := repohost.ParseProvider(c.RepositoryHost)
	if providerErr != nil {
		fail("repohost: %v", providerErr)
	} else {
		problems = append(problems, c.validateRepositoryHost(provider)...)
	}
	problems = append(problems, c.validateInstallation("github.issues", "the issue repository", c.GitHub.Issues.Repository, c.GitHub.Issues.InstallationID)...)
	problems = append(problems, c.validateInstallation("github.connectors", "the upstream connectors library", c.GitHub.Connectors.Repository, c.GitHub.Connectors.InstallationID)...)

	// Project scopes run only images pinned by digest. Local Kind also runs
	// Temporal and MinIO inside each scope.
	images := [][2]string{{"live.dexImage", c.Live.DexImage}}
	if c.LocalKind {
		images = append(images, [2]string{"live.temporalImage", c.Live.TemporalImage}, [2]string{"live.s3Image", c.Live.S3Image})
	}
	for _, image := range images {
		if !infra.ImmutableImage.MatchString(image[1]) {
			fail("%s must be an image reference pinned by digest", image[0])
		}
	}

	if c.LocalKind {
		if c.Builds.LocalURL == "" {
			fail("builds.localURL is required")
		}
		if !between(c.Routes.LocalPort, 1, 65535) {
			fail("routes.localPort must be a TCP port")
		}
		if !validOrigin(c.Routes.GatewayURL) {
			fail("routes.gatewayURL must be an http or https origin")
		}
	} else {
		problems = append(problems, c.validateHosted()...)
	}
	if c.Runtime == RuntimeDocker {
		problems = append(problems, c.validateNative(environment, provider)...)
	} else if c.Native != (NativeConfig{}) {
		fail("native is accepted only with runtime docker")
	}
	return errors.Join(problems...)
}

// validateAttachments checks the bounds of attachments: what one file, one
// message and one context segment may hold, how long uploads wait and stay
// staged, the quotas and the rendition cache.
func (c Config) validateAttachments() []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	a := c.Attachments
	// The Session refuses a message with more than ten attachments.
	if !between(a.MaxPerMessage, 1, 10) {
		fail("attachments.maxPerMessage must be an integer from 1 to 10")
	}
	if !between(a.ImageLongEdge, 256, 2000) {
		fail("attachments.imageLongEdge must be an integer from 256 to 2000")
	}
	if !between(a.ImageMaxBytes, 64<<10, 4<<20) {
		fail("attachments.imageMaxBytes must be from 64 KiB to 4 MiB")
	}
	if !between(a.DocumentMaxBytes, 1<<20, 30<<20) {
		fail("attachments.documentMaxBytes must be from 1 MiB to 30 MiB, below the %d MiB object limit", objectstore.MaxObjectBytes>>20)
	}
	if !between(a.JSONMaxBytes, 64<<10, fileextract.MaxJSONBytes) {
		fail("attachments.jsonMaxBytes must be from 64 KiB to %d MiB", fileextract.MaxJSONBytes>>20)
	}
	if !between(a.MessageInlineText, 16<<10, 256<<10) {
		fail("attachments.messageInlineText must be from 16 KiB to 256 KiB")
	}
	// One message's attachments always fit a fresh segment, which a compaction opens for them: their
	// rendered blocks, images as base64, and their images.
	worst := conversation.MessageAttachmentBytes(a.MaxPerMessage, a.ImageMaxBytes, int64(a.MessageInlineText))
	if !between(a.SegmentBytes, 1<<20, 24<<20) {
		fail("attachments.segmentBytes must be from 1 MiB to 24 MiB")
	} else if worst > a.SegmentBytes {
		fail("one message of attachments.maxPerMessage attachments renders up to %d bytes (images of attachments.imageMaxBytes as base64, "+
			"documents within attachments.messageInlineText), which must fit attachments.segmentBytes (%d)", worst, a.SegmentBytes)
	}
	if !between(a.SegmentImages, 1, 90) {
		fail("attachments.segmentImages must be an integer from 1 to 90")
	} else if a.MaxPerMessage > a.SegmentImages {
		fail("attachments.maxPerMessage must not exceed attachments.segmentImages, so that the images of one message fit a fresh segment")
	}
	if !between(a.UploadWait, time.Minute, 24*time.Hour) {
		fail("attachments.uploadWait must be a duration from 1m0s to 24h0m0s")
	}
	if !between(a.StagingRetention, time.Hour, 72*time.Hour) {
		fail("attachments.stagingRetention must be a duration from 1h0m0s to 72h0m0s")
	}
	if !between(a.TeamSpaceStagedFiles, 10, 2000) {
		fail("attachments.teamSpaceStagedFiles must be an integer from 10 to 2000")
	}
	if !between(a.TeamSpaceStagedBytes, 100<<20, 20<<30) {
		fail("attachments.teamSpaceStagedBytes must be from 100 MiB to 20 GiB")
	}
	if !between(a.ProjectLifetimeFiles, 10, 10_000) {
		fail("attachments.projectLifetimeFiles must be an integer from 10 to 10000")
	}
	if !between(a.ProjectLifetimeBytes, 100<<20, 20<<30) {
		fail("attachments.projectLifetimeBytes must be from 100 MiB to 20 GiB")
	}
	if largest := max(a.ImageMaxBytes, a.DocumentMaxBytes, a.JSONMaxBytes); largest > a.TeamSpaceStagedBytes || largest > a.ProjectLifetimeBytes {
		fail("attachments.teamSpaceStagedBytes and attachments.projectLifetimeBytes must each hold the largest file a kind admits")
	}
	if !between(a.RenditionCacheBytes, 16<<20, 1<<30) {
		fail("attachments.renditionCacheBytes must be from 16 MiB to 1 GiB")
	}
	return problems
}

// validateRemoteAgent checks the separate Bridge listener against every
// listener in the effective configuration; Native additionally requires loopback.
// modelID admits an API model ID as model connectors report it.
var modelID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,127}$`)

// validateEvolve checks the export bounds, the price table, the review access
// and the local lanes' short goal durations.
func (c Config) validateEvolve() []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	e := c.Evolve
	for _, bound := range []struct {
		name                  string
		value, minimum, limit int
	}{
		{"maxRunsPerExport", e.MaxRunsPerExport, 1, 20_000}, {"maxHistoryEventsPerRun", e.MaxHistoryEventsPerRun, 100, 10_000},
		{"maxReadsPerExport", e.MaxReadsPerExport, 100, 200_000}, {"maxConcurrentExports", e.MaxConcurrentExports, 1, 8},
		{"keepExports", e.KeepExports, 1, 25},
	} {
		if !between(bound.value, bound.minimum, bound.limit) {
			fail("evolve.%s must be an integer from %d to %d", bound.name, bound.minimum, bound.limit)
		}
	}
	seen := map[string]bool{}
	for _, price := range e.ModelPrices {
		if !modelID.MatchString(price.Model) || seen[price.Model] {
			fail("evolve.modelPrices must name each model once by its API model ID")
		}
		seen[price.Model] = true
		if !between(price.InputPerMTok, 0, 10_000) || !between(price.OutputPerMTok, 0, 10_000) {
			fail("evolve.modelPrices of %s must be dollars per million tokens from 0 to 10000", price.Model)
		}
	}
	if _, err := time.Parse(time.DateOnly, e.PriceTableAsOf); len(e.ModelPrices) > 0 && err != nil {
		fail("evolve.priceTableAsOf must date the price table as YYYY-MM-DD")
	}
	if err := e.ReviewAccess.Validate(); err != nil {
		fail("evolve.reviewAccess: %v", err)
	}
	if e.AllowShortDurations && !c.LocalKind {
		fail("evolve.allowShortDurations is restricted to the local lanes")
	}
	if len(e.ReviewTeamSpaces) > MaxEvolveReviewTeamSpaces {
		fail("evolve.reviewTeamSpaces must contain at most %d TeamSpace IDs", MaxEvolveReviewTeamSpaces)
	}
	listed := map[string]bool{}
	for _, teamSpace := range e.ReviewTeamSpaces {
		if flowid.ValidateTeamSpaceID(teamSpace) != nil || listed[teamSpace] {
			fail("evolve.reviewTeamSpaces must name each TeamSpace ID once")
		}
		listed[teamSpace] = true
	}
	return problems
}

func (c Config) validateRemoteAgent() []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	r := c.RemoteAgent
	if _, port, err := net.SplitHostPort(r.Listen); err != nil || !validPort(port) {
		fail("remoteAgent.listen must be a host:port address")
	} else if c.Runtime == RuntimeKubernetes && port != strconv.Itoa(RemoteAgentPort) {
		fail("remoteAgent.listen must use port %d with runtime kubernetes", RemoteAgentPort)
	}
	if !between(r.MaxLongPolls, 1, 10000) {
		fail("remoteAgent.maxLongPolls must be an integer from 1 to 10000")
	}
	if !between(r.MaxConcurrentReads, 1, 64) {
		fail("remoteAgent.maxConcurrentReads must be an integer from 1 to 64")
	}
	if !between(r.MaxConcurrentReports, 1, 256) {
		fail("remoteAgent.maxConcurrentReports must be an integer from 1 to 256")
	}
	if version := "v" + r.MinimumBridgeVersion; !semver.IsValid(version) || semver.Canonical(version) != version {
		fail("remoteAgent.minimumBridgeVersion must be a semantic version without a v prefix")
	}
	if !filepath.IsAbs(r.DownloadsDir) {
		fail("remoteAgent.downloadsDir must be an absolute environment artifact directory")
	}
	if r.AcceptDevBridge && !c.LocalKind {
		fail("remoteAgent.acceptDevBridge is restricted to explicit Local Native or Kind")
	}
	for _, other := range []struct{ name, value string }{
		{"server.grpcAddress", c.Server.GRPCAddress}, {"server.healthAddress", c.Server.HealthAddress},
		{"dex.serverAddress", c.Dex.ServerAddress},
		{"connections.readListen", c.Connections.ReadListen}, {"connections.slackRelay.listenAddress", c.Connections.SlackRelay.ListenAddress},
		{"localgit.listen", c.LocalGit.Listen}, {"native.routesListen", c.Native.RoutesListen},
	} {
		if other.value != "" && samePort(r.Listen, other.value) {
			fail("remoteAgent.listen must not share the port of %s", other.name)
		}
	}
	return problems
}

// validateConnections checks the credential read endpoint, the address
// policy of provider calls and the platform OAuth clients.
func (c Config) validateConnections() []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	settings := c.Connections
	if settings.MockLLM && (!c.LocalKind || !c.Agent.MockModel) {
		fail("connections.mockLLM requires a local lane with agent.mockModel")
	}
	if _, port, err := net.SplitHostPort(settings.ReadListen); err != nil || !validPort(port) {
		fail("connections.readListen must be a host:port address")
	} else if c.Runtime == RuntimeKubernetes && port != strconv.Itoa(infra.CredentialReadPort) {
		fail("connections.readListen must use port %d with runtime kubernetes, the port the chart and the project network policies admit", infra.CredentialReadPort)
	}
	for _, other := range []struct{ name, value string }{
		{"server.grpcAddress", c.Server.GRPCAddress}, {"server.healthAddress", c.Server.HealthAddress},
	} {
		if samePort(settings.ReadListen, other.value) {
			fail("connections.readListen must not share the port of %s", other.name)
		}
	}
	if err := connections.ValidateReadURL(settings.ReadURL); err != nil {
		fail("connections.readURL %v", err)
	}
	if settings.OAuthCallbackOrigin != "" && (!validOrigin(settings.OAuthCallbackOrigin) || !c.LocalKind) {
		fail("connections.oauthCallbackOrigin must be an http or https origin, and only the local lanes set it")
	}
	if settings.AllowInsecureOAuthEndpoints && !c.LocalKind {
		fail("connections.allowInsecureOAuthEndpoints is restricted to the local lanes")
	}
	for _, key := range slices.Sorted(maps.Keys(settings.PlatformClients)) {
		client := settings.PlatformClients[key]
		if err := connections.ValidatePlatformClient(key, client.ClientID, client.Scopes); err != nil {
			fail("connections.platformClients: %v", err)
		}
	}
	return append(problems, c.validateSlackRelay()...)
}

// validateSlackRelay checks the Slack event relay's listener, queue and
// deliveries.
func (c Config) validateSlackRelay() []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	relay := c.Connections.SlackRelay
	if _, port, err := net.SplitHostPort(relay.ListenAddress); err != nil || !validPort(port) {
		fail("connections.slackRelay.listenAddress must be a host:port address")
	} else if c.Runtime == RuntimeKubernetes && port != strconv.Itoa(SlackEventsPort) {
		fail("connections.slackRelay.listenAddress must use port %d with runtime kubernetes, the port the chart and its load balancer route", SlackEventsPort)
	}
	for _, other := range []struct{ name, value string }{
		{"server.grpcAddress", c.Server.GRPCAddress}, {"server.healthAddress", c.Server.HealthAddress},
		{"connections.readListen", c.Connections.ReadListen},
	} {
		if samePort(relay.ListenAddress, other.value) {
			fail("connections.slackRelay.listenAddress must not share the port of %s", other.name)
		}
	}
	queue := relay.Queue
	if queue.Implementation != eventqueue.ImplementationMemory {
		fail("connections.slackRelay.queue.implementation must be %s", eventqueue.ImplementationMemory)
	}
	if !between(queue.MaxEventBytes, 1<<10, 16<<20) {
		fail("connections.slackRelay.queue.maxEventBytes must be from 1 KiB to 16 MiB")
	}
	if !between(queue.MaxBytes, queue.MaxEventBytes, 1<<30) {
		fail("connections.slackRelay.queue.maxBytes must be from maxEventBytes to 1 GiB")
	}
	if !between(queue.MaxEvents, 1, 1_000_000) {
		fail("connections.slackRelay.queue.maxEvents must be from 1 to 1000000")
	}
	if !between(queue.MaxScopeEvents, 1, queue.MaxEvents) {
		fail("connections.slackRelay.queue.maxScopeEvents must be from 1 to maxEvents")
	}
	if !between(relay.Delivery.Timeout, time.Second, time.Minute) {
		fail("connections.slackRelay.delivery.timeout must be from 1s to 1m")
	}
	if !between(relay.Delivery.Concurrency, 1, 256) {
		fail("connections.slackRelay.delivery.concurrency must be from 1 to 256")
	}
	return problems
}

// samePort reports two listen addresses on one port.
func samePort(first, second string) bool {
	_, firstPort, firstErr := net.SplitHostPort(first)
	_, secondPort, secondErr := net.SplitHostPort(second)
	return firstErr == nil && secondErr == nil && firstPort == secondPort
}

// validateRepositoryHost checks the settings of the chosen Git host. GitHub needs
// its App IDs in every environment; the local Git host needs its directory and
// addresses, only exists in the local lanes, and leaves the GitHub App settings
// unused.
func (c Config) validateRepositoryHost(provider repohost.Provider) []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	switch provider {
	case repohost.ProviderGitHub:
		if c.GitHub.Owner == "" {
			fail("github.owner is required")
		}
		if c.GitHub.AppID <= 0 {
			fail("github.appId must be a positive GitHub App ID")
		}
		if c.GitHub.InstallationID <= 0 {
			fail("github.installationId must be a positive GitHub installation ID")
		}
	case repohost.ProviderLocalGit:
		if !c.LocalKind {
			fail("repohost localgit is accepted only in Local Kind and Native")
		}
		if !filepath.IsAbs(c.LocalGit.Dir) {
			fail("localgit.dir must be an absolute path")
		}
		if _, port, err := net.SplitHostPort(c.LocalGit.Listen); err != nil || !validPort(port) {
			fail("localgit.listen must be a host:port address")
		}
		if !validOrigin(c.LocalGit.CloneBaseURL) {
			fail("localgit.cloneBaseURL must be an http or https origin")
		}
	}
	return problems
}

// gitHubRepository admits a GitHub repository owner/name: an owner of letters,
// digits and hyphens, and a name of letters, digits, '.', '_' and '-'.
var gitHubRepository = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}/[A-Za-z0-9._-]{1,100}$`)

// validateInstallation checks a repository that the server reaches through
// another installation of the App, the issue repository or the upstream
// connectors library: the keys key.repository and key.installationId both or
// neither, an owner/name and a positive installation of the App that
// github.appId names, whatever the repository host.
func (c Config) validateInstallation(key, role, repository string, installationID int64) []error {
	if repository == "" && installationID == 0 {
		return nil
	}
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	if repository == "" || installationID == 0 {
		fail("%[1]s.repository and %[1]s.installationId are set together or not at all", key)
	}
	if _, name, _ := strings.Cut(repository, "/"); repository != "" && (!gitHubRepository.MatchString(repository) || name == "." || name == "..") {
		fail("%s.repository must be a GitHub repository owner/name", key)
	}
	if installationID < 0 {
		fail("%s.installationId must be a positive GitHub installation ID", key)
	}
	if c.GitHub.AppID <= 0 {
		fail("%s requires github.appId, the App installed on %s", key, role)
	}
	return problems
}

// validPort admits a TCP port number.
func validPort(port string) bool {
	number, err := strconv.Atoi(port)
	return err == nil && between(number, 1, 65535)
}

// validateHosted checks what every environment but Local Kind needs: the cloud
// account, the CodeBuild project and the project cloud account. The hosted
// provider and builder check each value against the deployment catalog when
// they start.
func (c Config) validateHosted() []error {
	var problems []error
	if !accountID.MatchString(c.AWS.AccountID) {
		problems = append(problems, errors.New("aws.accountId must be a 12-digit AWS account ID"))
	}
	if !between(c.ProjectCloud.TemporalRetentionDays, 1, 90) {
		problems = append(problems, errors.New("projectCloud.temporalRetentionDays must be an integer from 1 to 90"))
	}
	if host, port, err := net.SplitHostPort(c.ProjectCloud.TemporalAddress); err != nil || host != "xtemporal-frontend.temporal.svc.cluster.local" || port != "7233" {
		problems = append(problems, errors.New("projectCloud.temporalAddress must be the private xtemporal frontend on port 7233"))
	}

	hostedBuild, cloud := c.Builds.Hosted, c.ProjectCloud
	for _, setting := range []struct{ name, value string }{
		{"storage.artifactKMSKeyID", c.Storage.ArtifactKMSKeyID},
		{"builds.hosted.project", hostedBuild.Project}, {"builds.hosted.repository", hostedBuild.Repository}, {"builds.hosted.bucket", hostedBuild.Bucket},
		{"builds.hosted.prefix", hostedBuild.Prefix}, {"builds.hosted.kmsKeyARN", hostedBuild.KMSKeyARN},
		{"builds.hosted.toolImage", hostedBuild.ToolImage}, {"builds.hosted.hostImage", hostedBuild.HostImage},
		{"projectCloud.eksClusterName", cloud.EKSClusterName}, {"projectCloud.bucketPrefix", cloud.BucketPrefix}, {"projectCloud.kmsKeyARN", cloud.KMSKeyARN},
		{"projectCloud.permissionsBoundaryARN", cloud.PermissionsBoundaryARN},
	} {
		if setting.value == "" {
			problems = append(problems, fmt.Errorf("%s is required outside Local Kind", setting.name))
		}
	}
	return problems
}

// validateNative admits Local Native: runtime docker in the local test lane,
// whose processes listen only on the loopback and whose containers reach them
// only through host.docker.internal. The topology is these relations, not
// fixed ports: the Worker target and the clone origin are the Worker's and the
// local Git host's own ports as containers see them.
func (c Config) validateNative(environment deploymentnames.Environment, provider repohost.Provider) []error {
	if !c.LocalKind || environment != deploymentnames.EnvironmentTest {
		return []error{errors.New("runtime docker requires localKind and the test environment")}
	}
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }
	native := c.Native
	if !nativeInstance.MatchString(native.Instance) {
		fail("native.instance must be 1 to 12 lowercase letters or digits")
	}
	if !nativeRunID.MatchString(native.RunID) {
		fail("native.runId must be the launcher's run identity: 8 to 64 lowercase letters, digits or hyphens")
	}
	for _, path := range []struct{ name, value string }{{"native.stateDir", native.StateDir}, {"native.dockerSocket", native.DockerSocket}} {
		if !filepath.IsAbs(path.value) {
			fail("%s must be an absolute path", path.name)
		}
	}
	if prefix, err := netip.ParsePrefix(native.NetworkPrefix); err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsPrivate() || prefix.Masked() != prefix || prefix.Bits() < 8 || prefix.Bits() > 24 {
		fail("native.networkPrefix must be a private IPv4 network from /8 to /24, such as 10.233.0.0/16")
	}
	if host, port, err := net.SplitHostPort(native.Registry); err != nil || (host != "localhost" && host != loopback) || !validPort(port) {
		fail("native.registry must be localhost:<port>, which the engine pushes to and pulls from without TLS")
	}
	listeners := []struct{ name, value string }{
		{"server.grpcAddress", c.Server.GRPCAddress}, {"server.healthAddress", c.Server.HealthAddress},
		{"dex.serverAddress", c.Dex.ServerAddress}, {"native.routesListen", native.RoutesListen}, {"connections.readListen", c.Connections.ReadListen},
		{"connections.slackRelay.listenAddress", c.Connections.SlackRelay.ListenAddress},
		{"remoteAgent.listen", c.RemoteAgent.Listen},
	}
	if provider == repohost.ProviderLocalGit {
		listeners = append(listeners, struct{ name, value string }{"localgit.listen", c.LocalGit.Listen})
	}
	for _, listener := range listeners {
		if host, port, err := net.SplitHostPort(listener.value); err != nil || host != loopback || !validPort(port) {
			fail("%s must be %s:<port>: Local Native listens and dials only on the loopback", listener.name, loopback)
		}
	}
	for _, origin := range []struct{ name, value string }{
		{"routes.gatewayURL", c.Routes.GatewayURL}, {"native.gatewayAPI", native.GatewayAPI}, {"storage.s3Endpoint", c.Storage.S3Endpoint},
		{"storage.browserEndpoint", c.Storage.BrowserEndpoint}, {"builds.localURL", c.Builds.LocalURL},
	} {
		if parsed, err := url.Parse(origin.value); err != nil || !validOrigin(origin.value) || parsed.Hostname() != loopback {
			fail("%s must be an http origin on %s", origin.name, loopback)
		}
	}
	if _, port, err := net.SplitHostPort(c.Connections.ReadListen); err == nil && c.Connections.ReadURL != "http://"+net.JoinHostPort(infra.HostFromContainers, port)+connections.CredentialReadPath {
		fail("connections.readURL must be http://%s%s, where the applications reach the credential read endpoint", net.JoinHostPort(infra.HostFromContainers, port), connections.CredentialReadPath)
	}
	if _, port, err := net.SplitHostPort(c.LocalGit.Listen); provider == repohost.ProviderLocalGit && err == nil && c.LocalGit.CloneBaseURL != "http://"+net.JoinHostPort(infra.HostFromContainers, port) {
		fail("localgit.cloneBaseURL must be http://%s, where a Sandbox reaches the local Git host", net.JoinHostPort(infra.HostFromContainers, port))
	}
	return problems
}

func between[T cmp.Ordered](value, minimum, maximum T) bool {
	return value >= minimum && value <= maximum
}

// validOrigin admits an http or https origin: a scheme and a host, no user, path,
// query or fragment. Studio's public origin, where OAuth providers return actors,
// and the origin Sandboxes clone from have this form.
func validOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host != "" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" &&
		(parsed.Scheme == "http" || parsed.Scheme == "https")
}

func (c Config) validateLoginHost() []error {
	h := c.Accounts.LoginHost
	var problems []error
	fail := func(message string) { problems = append(problems, errors.New(message)) }
	for _, budget := range []struct {
		value, maximum int
		name           string
	}{
		{h.IssuePerMinute, DefaultLoginIssuePerMinute, "issuePerMinute"},
		{h.IssueBurst, DefaultLoginIssueBurst, "issueBurst"},
		{h.ExchangePerMinute, DefaultLoginExchangePerMinute, "exchangePerMinute"},
		{h.ExchangeBurst, DefaultLoginExchangeBurst, "exchangeBurst"},
	} {
		if budget.value < 1 || budget.value > budget.maximum {
			fail(fmt.Sprintf("accounts.loginHost.%s must be from 1 to %d", budget.name, budget.maximum))
		}
	}
	if h.HandoffEnabled && !h.ExchangeEnabled {
		fail("accounts.loginHost.exchangeEnabled is required while handoffEnabled")
	}
	if !h.HandoffEnabled && !h.ExchangeEnabled {
		return problems
	}
	if !loginOrigin(h.PublicOrigin, c.LocalKind) || h.PublicOrigin == c.Studio.PublicOrigin {
		fail("accounts.loginHost.publicOrigin must be a distinct exact login origin")
	}
	if len(h.Clients) == 0 || len(h.Clients) > 2 {
		fail("accounts.loginHost.clients must register one or two fixed products")
	}
	studio, exists := h.Clients["studio"]
	if !exists || !studio.Enabled || studio.Origin != c.Studio.PublicOrigin ||
		studio.RedirectURI != studio.Origin+"/api/auth/callback" || !loginOrigin(studio.Origin, c.LocalKind) {
		fail("accounts.loginHost.clients.studio must match studio.publicOrigin and its fixed callback")
	}
	for id, client := range h.Clients {
		if id != "studio" && id != "engine" {
			fail("accounts.loginHost.clients contains an unknown product")
		}
		if client.Enabled && (!loginOrigin(client.Origin, c.LocalKind) || client.RedirectURI != client.Origin+"/api/auth/callback" || client.Origin == h.PublicOrigin) {
			fail("accounts.loginHost.clients contains an invalid origin or callback")
		}
		if id == "engine" && client.Enabled && client.Origin == studio.Origin {
			fail("accounts.loginHost.clients.engine must have its own origin")
		}
	}
	return problems
}

func loginOrigin(origin string, local bool) bool {
	if !validOrigin(origin) || strings.ContainsAny(origin, "\\%\r\n\t") {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.String() != origin || parsed.Hostname() == "" || parsed.Hostname() != strings.ToLower(parsed.Hostname()) ||
		strings.IndexFunc(parsed.Hostname(), func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-')
		}) >= 0 {
		return false
	}
	if local {
		return parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || strings.HasSuffix(parsed.Hostname(), ".localhost"))
	}
	return parsed.Scheme == "https" && parsed.Port() == ""
}
