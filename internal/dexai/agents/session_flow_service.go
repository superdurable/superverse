// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/dexai/agents/platformissue"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/backgroundcommand"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
)

// SessionFlowService is the Session Service: the operations of an
// authenticated account on a Project's one Session and its Preview, each
// addressed by the Project, for the API, and the Flow dependencies, which the
// Flows reach through the interfaces their own packages declare. It works on
// the Flow clients of the Flows it coordinates.
type SessionFlowService struct {
	RemoteConnections *remoteagentdao.DAO
	Projects          *projects.ProjectFlowClient
	Provisions        *provision.SandboxProvisionFlowClient
	// Workspaces fronts the SessionWorkspaceFlow that owns the Session's
	// instances (session_flow_service_workspace.go).
	Workspaces *sessionworkspace.SessionWorkspaceFlowClient
	Previews   *preview.PreviewFlowClient
	Agent      *sessionagent.SessionAgentFlowClient
	Subagents  *subagent.SubagentFlowClient
	// Contributions fronts the ConnectorContributionFlows of the Session's
	// connector contributions (session_flow_service_connectors.go).
	Contributions *contribution.ConnectorContributionFlowClient
	// Instances and PreviewData release the resources of a deleting Project's
	// Session by label: its instance's objects and its Preview data.
	Instances   sandbox.InstanceProvider
	PreviewData *preview.PreviewConfiguration
	Logger      *slog.Logger
	// Issues reports the platform faults of Sessions
	// (session_flow_service_issues.go).
	Issues *platformissue.PlatformIssueFlowClient
	// BackgroundCommands stops the watches of a closing Session's background
	// commands (session_flow_service_tasks.go).
	BackgroundCommands *backgroundcommand.BackgroundCommandFlowClient
}

var (
	_ sessionagent.SessionAgentFlowDependency          = (*SessionFlowService)(nil)
	_ provision.SandboxProvisionFlowDependency         = (*SessionFlowService)(nil)
	_ sessionworkspace.SessionWorkspaceFlowDependency  = (*SessionFlowService)(nil)
	_ preview.PreviewFlowDependency                    = (*SessionFlowService)(nil)
	_ subagent.SubagentFlowDependency                  = (*SessionFlowService)(nil)
	_ contribution.ConnectorContributionFlowDependency = (*SessionFlowService)(nil)
	_ projects.SessionLifecycle                        = (*SessionFlowService)(nil)
	_ projectconfig.PreviewReader                      = (*SessionFlowService)(nil)
	_ releases.DeliveryFeedback                        = (*SessionFlowService)(nil)
	_ releases.PreviewValidationReader                 = (*SessionFlowService)(nil)
)

// ErrSessionNotActive rejects a workspace or Preview demand for an archived
// Session: such a Session never starts another Sandbox instance.
var ErrSessionNotActive = errors.New("session is archived")

// Snapshot joins the Session with its instance, which the
// SessionWorkspaceFlow owns, and the Project's Preview. Workspace is nil while
// the Session never sent a workspace command, PreviewState while the Project
// never ran a Preview command.
type Snapshot struct {
	// RemoteLastSeenAt is transient SQL liveness for this exact executor only.
	RemoteLastSeenAt *time.Time
	ExecutionFailure agentloop.ExecutionFailure
	Session          agentloop.SessionSummary
	Agent            *agentloop.AgentSnapshot
	Workspace        *sessionworkspace.Snapshot
	PreviewState     *preview.Snapshot
	// Contributions are the Session's connector contributions, read from
	// their ConnectorContributionFlows, in the order the Session named them.
	Contributions []contribution.Snapshot
}

// Preview is always present, also for a Project without a Preview.
func (v Snapshot) Preview() preview.Snapshot { return previewView(v.Session, v.PreviewState) }

// StartForProject starts the READY Project's one Session, from ProjectFlow's
// StartProjectSession, its only creation trigger. The Session starts with the
// default profile on the Project's AppManifest commit, on the model the
// creator chose or the server's default, with the creator's first message,
// which its start Step admits once, and, for a Project created from an
// example, that example's brief: picked, or matched when the creation
// recorded what the server matched the first message on. A retried or
// replayed start joins the accepted execution.
func (s *SessionFlowService) StartForProject(ctx context.Context, project projects.Project, initial *projects.SessionStartRequest) error {
	identity := agentloop.SessionIdentity{ProjectID: project.ProjectID, Profile: agentloop.ProfileSessionV1, HeadCommit: project.AppManifest.SourceCommit}
	request := s.Agent.SessionRequest(identity)
	if initial != nil && initial.Model != "" {
		request.Model = conversation.Model(initial.Model)
	}
	if initial != nil && initial.Text != "" {
		author := conversation.Actor{AccountID: initial.AuthorAccountID, Email: initial.AuthorEmail}
		request.FirstMessage = &conversation.PendingUserMessage{MessageID: conversation.MessageID(initial.MessageID), Value: conversation.UserMessage{Actor: &author, Content: initial.Text}}
	}
	if id, tree, fromExample := apptemplate.ParseExampleRevision(project.TemplateRevision); fromExample {
		request.ExampleBrief = exampleBrief(id, tree)
		if initial != nil && initial.MatchedExample != nil {
			request.ExampleBrief.Choice, request.ExampleBrief.MatchedOn = agentloop.ExampleChoiceMatched, initial.MatchedExample.Facets
			request.ExampleBrief.Card = ""
		}
	}
	return s.Agent.StartAccepted(ctx, request)
}

// exampleBrief is the brief of the example a Project was created from, as this
// server build embeds it. A build that holds the example at another tree, or
// not at all, starts the Session with the problem instead of the card: the
// card would not describe the committed source.
func exampleBrief(id, tree string) *agentloop.ExampleBrief {
	brief := &agentloop.ExampleBrief{ExampleID: id, Tree: tree, Title: id, Choice: agentloop.ExampleChoicePicked}
	example, found := apptemplate.FindExample(id)
	template, err := apptemplate.ExampleTemplate(id)
	switch {
	case !found || err != nil:
		brief.Problem = "this server build holds no example " + id
	case template.TreeID != tree:
		brief.Title, brief.Problem = example.Title, "this server build holds the example at the tree "+template.TreeID
	default:
		brief.Title, brief.Card = example.Title, string(example.Card)
	}
	return brief
}

// SessionCaller is the authenticated author and authorized role of a Session command.
type SessionCaller struct {
	FlowID agentloop.FlowID
	Actor  conversation.Actor
	Role   projects.Role
}

// AuthorizeSession builds the author from the authenticated account and the Project's authorized role.
func (s *SessionFlowService) AuthorizeSession(ctx context.Context, account accounts.AccountSummary, project string) (SessionCaller, error) {
	started := time.Now()
	projection, err := s.Projects.Authorize(ctx, account.AccountID, project, projects.CapabilityBuild)
	if err != nil {
		s.logSessionReadFailure(ctx, "authorize_project", started, err)
		return SessionCaller{}, err
	}
	started = time.Now()
	if _, err = s.readSummary(ctx, project); err != nil {
		s.logSessionReadFailure(ctx, "read_session_identity", started, err)
		return SessionCaller{}, err
	}
	return SessionCaller{FlowID: agentloop.SessionFlowID(project), Actor: conversation.Actor{AccountID: account.AccountID, Email: account.CanonicalEmail}, Role: projection.CallerRole}, nil
}

// AuthorizeScope admits the account to the Session's Project without reading
// the Session: the check a Session Watch repeats while it streams.
func (s *SessionFlowService) AuthorizeScope(ctx context.Context, account, project string) error {
	return s.authorizeSessionScope(ctx, account, project)
}

// authorizeSessionScope admits the account to the Session's Project. Every
// API operation on the Session begins here.
func (s *SessionFlowService) authorizeSessionScope(ctx context.Context, account, project string) error {
	started := time.Now()
	if err := s.authorizeProject(ctx, account, project); err != nil {
		s.logSessionReadFailure(ctx, "authorize_project", started, err)
		return err
	}
	return nil
}

// readAuthorizedSession authorizes the account and reads the Session's summary.
// The summary is shared only within this request, never cached across requests
// or Watch refreshes. Account authentication remains at the API boundary.
func (s *SessionFlowService) readAuthorizedSession(ctx context.Context, account, project string) (agentloop.SessionSummary, error) {
	if err := s.authorizeSessionScope(ctx, account, project); err != nil {
		return agentloop.SessionSummary{}, err
	}
	started := time.Now()
	summary, err := s.readSummary(ctx, project)
	if err != nil {
		s.logSessionReadFailure(ctx, "read_session_identity", started, err)
	}
	return summary, err
}

// readSummary reads the Project's Session after the caller has authorized the
// Project. It reads no chat messages, queues or Sandbox state. It is the read
// behind every Session authorization, so it never trusts a retained ACTIVE: a
// Session whose engine execution stopped is a
// *sessionagent.SessionExecutionStoppedError, which refuses every command.
func (s *SessionFlowService) readSummary(ctx context.Context, project string) (agentloop.SessionSummary, error) {
	return s.Agent.GetLiveSessionSummary(ctx, agentloop.SessionFlowID(project))
}

// Get reads the Project's Session with its conversation and its Preview.
func (s *SessionFlowService) Get(ctx context.Context, account, project string) (Snapshot, error) {
	if err := s.authorizeSessionScope(ctx, account, project); err != nil {
		return Snapshot{}, err
	}
	return s.readSnapshot(ctx, project)
}

// readSnapshot reads the Session of an authorized Project with its Preview.
func (s *SessionFlowService) readSnapshot(ctx context.Context, project string) (Snapshot, error) {
	started := time.Now()
	state, err := s.Agent.GetSnapshot(ctx, agentloop.SessionFlowID(project))
	if err != nil {
		s.logSessionReadFailure(ctx, "read_session_snapshot", started, err)
		return Snapshot{}, err
	}
	if state.Session.CloseReason == agentloop.CloseReasonAdmissionDenied.String() {
		return Snapshot{Session: state.Session}, nil
	}
	started = time.Now()
	workspaceState, err := s.readWorkspace(ctx, project)
	if err != nil {
		s.logSessionReadFailure(ctx, "read_session_workspace", started, err)
		return Snapshot{}, err
	}
	started = time.Now()
	previewState, err := s.readPreview(ctx, project)
	if err != nil {
		s.logSessionReadFailure(ctx, "read_preview", started, err)
		return Snapshot{}, err
	}
	snapshot := Snapshot{Session: state.Session, Agent: state.Agent, Workspace: workspaceState, PreviewState: previewState, ExecutionFailure: state.ExecutionFailure}
	if state.Agent != nil {
		executor := state.Agent.Description.Executor
		if executor.Mode == remoteexec.ModeRemote && executor.Remote != nil && s.RemoteConnections != nil {
			if snapshot.RemoteLastSeenAt, err = s.RemoteConnections.LastSeen(ctx, project, executor.Remote.ConnectionID); err != nil {
				s.logSessionReadFailure(ctx, "read_remote_last_seen", started, err)
				return Snapshot{}, err
			}
		}
		if snapshot.Contributions, err = s.readContributions(ctx, project, state.Agent.Description.ConnectorSlugs); err != nil {
			s.logSessionReadFailure(ctx, "read_connector_contributions", started, err)
			return Snapshot{}, err
		}
	}
	return snapshot, nil
}

// readWorkspace reads the Session's instance; nil means the Session never
// sent a workspace command.
func (s *SessionFlowService) readWorkspace(ctx context.Context, project string) (*sessionworkspace.Snapshot, error) {
	state, err := s.Workspaces.Get(ctx, project)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// readPreview reads the Project's Preview; nil means the Project never ran a
// Preview command.
func (s *SessionFlowService) readPreview(ctx context.Context, project string) (*preview.Snapshot, error) {
	state, err := s.Previews.Get(ctx, project)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// logSessionReadFailure logs only fixed categories and numeric status metadata.
// SDK error strings, Worker details and request identities may contain private
// application data.
func (s *SessionFlowService) logSessionReadFailure(ctx context.Context, phase string, started time.Time, err error) {
	category := "other"
	var service *dex.ServiceError
	var worker *dex.WorkerInvocationError
	var missing *dex.AttributeNotFoundError
	var mapping *dex.ValueMappingError
	var definition *dex.FlowDefinitionError
	switch {
	case errors.As(err, &worker):
		category = "sdk_worker_invocation"
	case errors.As(err, &service):
		category = "sdk_service"
	case errors.As(err, &missing):
		category = "sdk_attribute_missing"
	case errors.As(err, &mapping):
		category = "sdk_value_mapping"
	case errors.As(err, &definition):
		category = "sdk_flow_definition"
	case errors.Is(err, context.Canceled):
		category = "context_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		category = "context_deadline"
	}
	attrs := []slog.Attr{
		slog.String("phase", phase),
		slog.Int64("elapsed_ms", time.Since(started).Milliseconds()),
		slog.String("error_category", category),
		slog.Bool("context_canceled", errors.Is(ctx.Err(), context.Canceled)),
		slog.Bool("context_deadline", errors.Is(ctx.Err(), context.DeadlineExceeded)),
	}
	// A Worker invocation error can wrap a service error; both codes are logged.
	if errors.As(err, &service) {
		attrs = append(attrs, slog.Int("sdk_grpc_code", int(service.Code)), slog.Int("sdk_substatus", int(service.SubStatus)))
	}
	if worker != nil && worker.Worker != nil {
		attrs = append(attrs, slog.Int("worker_grpc_code", int(worker.Worker.Code)))
	}
	s.Logger.LogAttrs(ctx, slog.LevelWarn, "Session resource read failed", attrs...)
}
