// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"errors"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/app/apptemplate"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// projectServer serves ProjectService: Projects, the configuration of a Live
// Release or a Preview Session, and Live removal.
type projectServer struct {
	pb.UnimplementedProjectServiceServer
	*boundary.Boundary
	projectIDLength int
	projects        *projects.ProjectFlowClient
	members         *memberdao.MemberDAO
	teamSpaces      *teamspaces.TeamSpaceFlowClient
	setups          *setup.ProjectSetupFlowClient
	configuration   *projectconfig.Configuration
	deployments     *deployments.ProjectDeployFlowClient
	sessions        *agents.SessionFlowService
	recentEvents    RecentTriggerEvents
	models          SessionModels
}

// CreateProject starts a Project of a TeamSpace on a new private managed
// repository. ProjectFlow's first Step admits it in the TeamSpace, which
// requires the caller's membership unless the caller is the platform admin.
// The Session's first message and model travel with it (initialSession).
// Repeating a request returns the same Project.
func (s *projectServer) CreateProject(ctx context.Context, in *pb.CreateProjectRequest) (*pb.CreateProjectResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	if err = boundary.InvalidIf(in.GetName() == "" || len(in.GetName()) > 120, "project name must contain 1 to 120 bytes"); err != nil {
		return nil, err
	}
	if in.GetManagedRepository() == nil {
		return nil, status.Error(codes.InvalidArgument, "managed_repository is required")
	}
	if err = flowid.ValidateTeamSpaceID(in.GetTeamSpaceId()); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	initial, err := validInitialSession(in, account)
	if err != nil {
		return nil, err
	}
	if err = s.admitCreator(ctx, account, in.GetTeamSpaceId()); err != nil {
		return nil, err
	}
	if initial != nil && initial.Model != "" {
		if err = admitModel(ctx, s.Boundary, s.models, account.AccountID, conversation.Model(initial.Model)); err != nil {
			return nil, refusalStatus(err)
		}
	}
	projectID, setupID := flowid.ProjectCreationIDs(account.AccountID, in.GetRequestId(), s.projectIDLength)
	start := projects.ProjectStart{ProjectID: projectID, CreatorAccountID: account.AccountID, Name: in.GetName(), SetupFlowID: flowid.SetupFlowID(projectID, setupID),
		TemplateRevision: apptemplate.Current().Revision, TeamSpaceID: in.GetTeamSpaceId(), CreatorIsPlatformAdmin: accounts.IsAdministrator(account), InitialSession: initial}
	fingerprint := managedProjectCreationFingerprint(account.AccountID, in.GetRequestId(), in.GetName(), in.GetTeamSpaceId(), initial)
	start.Identity = dexflow.StartIdentity{RequestID: fingerprint, Fingerprint: fingerprint, ActorAccountID: account.AccountID}
	if err = s.projects.Create(ctx, start); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.CreateProjectResponse{ProjectId: projectID}, nil
}

// admitCreator refuses, before any Flow starts, a creator whom the read model
// shows outside a TeamSpace it holds; ProjectFlow's registration confirms the
// membership strongly. A TeamSpace the read model does not hold yet, such as
// one created a moment ago, is left to that registration.
func (s *projectServer) admitCreator(ctx context.Context, account accounts.AccountSummary, teamSpaceID string) error {
	if accounts.IsAdministrator(account) {
		return nil
	}
	role, err := s.members.TeamSpaceRole(ctx, teamSpaceID, account.AccountID, teamspaces.StatusDeleted)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		return nil
	case err != nil:
		return boundary.RPCStatus(err)
	case role == "":
		return status.Error(codes.PermissionDenied, "only a member of the TeamSpace creates its Projects")
	}
	return nil
}

// validInitialSession admits the Session's first turn of a creation before any
// Flow starts: a first message as SendMessage admits one, with its message ID,
// and a known model, which admitModel then authorizes. It is nil when the
// request names neither.
func validInitialSession(in *pb.CreateProjectRequest, account accounts.AccountSummary) (*projects.SessionStartRequest, error) {
	if in.GetInitialMessage() == "" && in.GetMessageId() == "" && in.GetModel() == pb.AgentModel_AGENT_MODEL_UNSPECIFIED {
		return nil, nil
	}
	initial := &projects.SessionStartRequest{}
	if in.GetInitialMessage() != "" || in.GetMessageId() != "" {
		if err := boundary.ValidateMessageID(in.GetMessageId()); err != nil {
			return nil, err
		}
		if err := boundary.InvalidIf(!validMessageContent(in.GetInitialMessage()), "invalid initial message content"); err != nil {
			return nil, err
		}
		initial.MessageID, initial.Text = in.GetMessageId(), in.GetInitialMessage()
		initial.AuthorAccountID, initial.AuthorEmail = account.AccountID, account.CanonicalEmail
	}
	if in.GetModel() != pb.AgentModel_AGENT_MODEL_UNSPECIFIED {
		model, isKnown := sessionModels[in.GetModel()]
		if !isKnown {
			return nil, status.Error(codes.InvalidArgument, "a supported model is required")
		}
		initial.Model = string(model)
	}
	return initial, nil
}

// refusalStatus is the public status of a refused model admission.
func refusalStatus(err error) error {
	if refusal, isRefused := errors.AsType[*commandRefusalError](err); isRefused {
		return refusal.public
	}
	return boundary.RPCStatus(err)
}

// managedProjectCreationFingerprint keeps the canonical CREATE request encoding
// stable independently of the durable repository binding's shape. The fixed
// repository values describe a template-created repository before provisioning;
// provider results and template revisions never enter the logical identity.
// The Session's first turn enters it by the digest of its message, so a retry
// that changes the message, its ID or the model conflicts; a request without
// one keeps the encoding it had before.
func managedProjectCreationFingerprint(accountID, requestID, name, teamSpaceID string, initial *projects.SessionStartRequest) string {
	type repositoryCreationIdentity struct {
		RepositoryID, InstallationID int64
		FullName, URL, DefaultBranch string
		Managed                      bool
		Generation                   uint64
	}
	type sessionCreationIdentity struct {
		Model, MessageID, MessageDigest string
	}
	var session *sessionCreationIdentity
	if initial != nil {
		session = &sessionCreationIdentity{Model: initial.Model, MessageID: initial.MessageID, MessageDigest: flowid.Fingerprint(initial.Text)}
	}
	return flowid.Fingerprint(struct {
		Mode, AccountID, RequestID, Name, TeamSpaceID string
		Repository                                    repositoryCreationIdentity
		Session                                       *sessionCreationIdentity `json:",omitempty"`
	}{"CREATE", accountID, requestID, name, teamSpaceID, repositoryCreationIdentity{Managed: true}, session})
}

// ListProjects lists from the read model, newest first, every Project of a
// TeamSpace for one of its members or the platform admin. An entry without
// the caller's role is reduced.
func (s *projectServer) ListProjects(ctx context.Context, in *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	statuses, err := boundary.DomainValues(in.GetStatuses())
	if err != nil {
		return nil, err
	}
	list := projects.ProjectList{AccountID: account.AccountID, PlatformAdmin: accounts.IsAdministrator(account), TeamSpaceID: in.GetTeamSpaceId(), Statuses: statuses}
	if err = s.requireTeamSpaceMember(ctx, list); err != nil {
		return nil, err
	}
	page, err := s.ReadPage("projects/"+account.AccountID+"/"+list.TeamSpaceID+"/"+strings.Join(statuses, ","), in)
	if err != nil {
		return nil, err
	}
	listed, next, err := s.projects.ListProjections(ctx, list, page.After, page.Size)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListProjectsResponse{NextPageToken: s.NextPageToken(page, next)}
	for _, projection := range listed {
		out.Projects = append(out.Projects, listedProjectSnapshot(projection))
	}
	return out, nil
}

// requireTeamSpaceMember admits the list of a TeamSpace's Projects for one of
// its members, as the read model shows, or the platform admin.
func (s *projectServer) requireTeamSpaceMember(ctx context.Context, list projects.ProjectList) error {
	if err := flowid.ValidateTeamSpaceID(list.TeamSpaceID); err != nil {
		return boundary.RPCStatus(err)
	}
	if list.PlatformAdmin {
		return nil
	}
	role, err := teamspaces.AwaitRow(ctx, s.teamSpaces, list.TeamSpaceID, func(ctx context.Context) (string, error) {
		return s.members.TeamSpaceRole(ctx, list.TeamSpaceID, list.AccountID, teamspaces.StatusDeleted)
	})
	switch {
	case err != nil:
		return boundary.RPCStatus(err)
	case role == "":
		return status.Error(codes.PermissionDenied, "TeamSpace access denied")
	}
	return nil
}

func (s *projectServer) GetProject(ctx context.Context, in *pb.GetProjectRequest) (*pb.GetProjectResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	projection, err := s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityView)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.GetProjectResponse{Project: projectSnapshot(projection.Project, projection.Setup, projection.CallerRole, projection.LiveVersion)}, nil
}

func (s *projectServer) DeleteProject(ctx context.Context, in *pb.DeleteProjectRequest) (*pb.DeleteProjectResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	// ProjectFlow admits the deletion at the revision the caller observed.
	authorized, err := s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityDelete)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	p, err := s.projects.RequestDeletion(ctx, in.GetProjectId(), in.GetRequestId(), in.GetExpectedRevision())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	snapshot, err := s.projectResponse(ctx, p, authorized.CallerRole)
	if err != nil {
		return nil, err
	}
	return &pb.DeleteProjectResponse{Project: snapshot}, nil
}

// WatchProject serves the read model's Project, also a DELETED one, which
// GetProject hides: its terminal snapshot ends the Watch. A Project created a
// moment ago is waited for until its row appears.
func (s *projectServer) WatchProject(in *pb.WatchProjectRequest, stream grpc.ServerStreamingServer[pb.WatchProjectResponse]) error {
	ctx := stream.Context()
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return err
	}
	project := in.GetProjectId()
	resource := watch.Resource[*pb.ProjectSnapshot, dexflow.ProgressEvent]{
		Connection: s.BindWatch(in, account.AccountID+"/"+flowid.ProjectFlowID(project)),
		// A deleted Project stays readable for its terminal snapshot.
		Authorize: func(ctx context.Context, accountID string) error {
			_, err := s.projects.ReadAuthorized(ctx, accountID, project, projects.CapabilityView)
			return err
		},
		ReadSnapshot: func(ctx context.Context, accountID string) (*pb.ProjectSnapshot, []watch.Source[dexflow.ProgressEvent], error) {
			projection, err := s.projects.ReadAuthorized(ctx, accountID, project, projects.CapabilityView)
			if err != nil {
				return nil, nil, err
			}
			snapshot := projectSnapshot(projection.Project, projection.Setup, projection.CallerRole, projection.LiveVersion)
			return snapshot, s.projectProgressSources(project, snapshot), nil
		},
		Terminal: func(p *pb.ProjectSnapshot) bool { return p.Status == pb.ProjectStatus_PROJECT_STATUS_DELETED },
		SendSnapshot: func(snapshot *pb.ProjectSnapshot, cursor string) error {
			return stream.Send(&pb.WatchProjectResponse{Event: &pb.WatchProjectResponse_Snapshot{Snapshot: snapshot}, ResumeCursor: cursor})
		},
		SendProgress: func(event dexflow.ProgressEvent, cursor string) error {
			return stream.Send(&pb.WatchProjectResponse{Event: &pb.WatchProjectResponse_Progress{Progress: resourceProgress(event)}, ResumeCursor: cursor})
		},
	}
	return boundary.RPCStatus(resource.Run(ctx))
}

// projectProgressSources are the Project's own progress Stream and, while its
// setup runs, the setup's.
func (s *projectServer) projectProgressSources(project string, p *pb.ProjectSnapshot) []watch.Source[dexflow.ProgressEvent] {
	if p.Status == pb.ProjectStatus_PROJECT_STATUS_DELETED {
		return nil
	}
	flowID := flowid.ProjectFlowID(project)
	sources := []watch.Source[dexflow.ProgressEvent]{watch.DexStreamSource(s.Dex, flowID, projects.ProjectProgress)}
	switch p.Setup.GetStatus() {
	case pb.SetupStatus_SETUP_STATUS_UNSPECIFIED, pb.SetupStatus_SETUP_STATUS_PENDING, pb.SetupStatus_SETUP_STATUS_COMPLETE, pb.SetupStatus_SETUP_STATUS_CANCELLED:
		// No setup runs.
	default:
		sources = append(sources, watch.DexStreamSource(s.Dex, flowid.SetupFlowID(project, p.Setup.GetSetupId()), setup.SetupProgress))
	}
	return sources
}

// resourceProgress is the best-effort progress of a Project, Release or
// Deployment Watch: the Step name, never the event's free-text Detail.
func resourceProgress(event dexflow.ProgressEvent) *pb.ResourceProgress {
	return &pb.ResourceProgress{Phase: event.Phase, Revision: event.Revision, CreatedAt: boundary.OptionalTimestamp(event.At)}
}

// The connector and environment configuration of a Live Release or a Preview
// Session. Studio edits it through the Flow API configuration services in
// flow_api.go.

func (s *projectServer) GetProjectConfiguration(ctx context.Context, in *pb.GetProjectConfigurationRequest) (*pb.GetProjectConfigurationResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	configuration, err := configurationWithItems(ctx, s.configuration, account.AccountID, in.GetTarget())
	if err != nil {
		return nil, boundary.ConfigurationRPCStatus(err)
	}
	return &pb.GetProjectConfigurationResponse{Configuration: configuration}, nil
}

func (s *projectServer) ValidateProjectConfiguration(ctx context.Context, in *pb.ValidateProjectConfigurationRequest) (*pb.ValidateProjectConfigurationResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	configuration, err := s.configuration.Validate(ctx, account.AccountID, in.GetTarget(), in.GetRequestId(), in.GetExpectedRevision())
	if err != nil {
		return nil, boundary.ConfigurationRPCStatus(err)
	}
	return &pb.ValidateProjectConfigurationResponse{Configuration: configuration}, nil
}

// RemoveLive asks the Project to remove the Live deployment at the Live
// pointer the caller observed. A Project without Live returns its snapshot
// unchanged.
func (s *projectServer) RemoveLive(ctx context.Context, in *pb.RemoveLiveRequest) (*pb.RemoveLiveResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	p, role, err := s.projects.RemoveLive(ctx, account.AccountID, in.GetProjectId(), in.GetRequestId(), in.GetDeploymentId(), in.GetExpectedLiveRevision())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	snapshot, err := s.projectResponse(ctx, p, role)
	if err != nil {
		return nil, err
	}
	return &pb.RemoveLiveResponse{Project: snapshot}, nil
}

// projectResponse is the snapshot that a write of a Project returns:
// ProjectFlow's state after the write, with its Setup's state and the
// caller's effective role.
func (s *projectServer) projectResponse(ctx context.Context, p projects.Project, callerRole projects.Role) (*pb.ProjectSnapshot, error) {
	var summary projects.SetupSummary
	if p.SetupFlowID != "" && p.ProjectStatus != projects.StatusDeleted {
		state, err := s.setups.Read(ctx, p.SetupFlowID)
		switch {
		case boundary.FlowNotFound(err):
		case err != nil:
			return nil, boundary.RPCStatus(err)
		default:
			summary = projects.SetupSummary{Status: state.Status, Phase: state.Phase, SourceCommit: state.SourceCommit, Failure: state.Failure, Revision: state.Revision}
		}
	}
	// A write answers with ProjectFlow's state, which holds no Release version;
	// the next read or Watch snapshot carries it.
	return projectSnapshot(p, summary, callerRole, repohost.CommitVersion{}), nil
}

// listedProjectSnapshot shows a listed Project: in full to a caller who holds
// a role in it, and otherwise only what a member of its TeamSpace sees of it,
// which leaves out what CapabilityView guards.
func listedProjectSnapshot(projection projects.ProjectProjection) *pb.ProjectSnapshot {
	full := projectSnapshot(projection.Project, projection.Setup, projection.CallerRole, projection.LiveVersion)
	if projection.CallerRole != "" {
		return full
	}
	reduced := &pb.ProjectSnapshot{ProjectId: full.ProjectId, Name: full.Name, Status: full.Status, Live: full.Live, TeamSpaceId: full.TeamSpaceId}
	if full.Setup != nil {
		reduced.Setup = &pb.SetupSnapshot{Status: full.Setup.Status}
	}
	return reduced
}

// projectSnapshot shows a Project with its setup, its Live pointer with the
// version of the Release it serves and the caller's effective role. A Project whose Setup shows no state yet and that
// has no AppManifest shows a PENDING setup.
func projectSnapshot(p projects.Project, summary projects.SetupSummary, callerRole projects.Role, liveVersion repohost.CommitVersion) *pb.ProjectSnapshot {
	binding := p.RepositoryBinding
	out := &pb.ProjectSnapshot{ProjectId: p.ProjectID, CreatorAccountId: p.CreatorAccountID, TeamSpaceId: p.TeamSpaceID, CallerRole: boundary.Enum[pb.ProjectRole](callerRole), Name: p.Name, Status: boundary.Enum[pb.ProjectStatus](p.ProjectStatus), Revision: p.Revision, TemplateRevision: p.TemplateRevision,
		Repository:  &pb.Repository{RepositoryId: binding.RepositoryID, FullName: binding.FullName, Url: binding.URL, DefaultBranch: binding.DefaultBranch},
		AppManifest: p.AppManifest, CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt), Failure: boundary.FailureSnapshot(p.LastFailure),
		SessionStartFailure: boundary.FailureSnapshot(p.SessionStartFailure)}
	if p.ActiveDeployment != nil {
		out.ActiveDeployment = &pb.ActiveDeploymentReference{DeploymentId: p.ActiveDeployment.DeploymentID, Generation: p.ActiveDeployment.Generation}
	}
	if live := p.CurrentLive; live != nil {
		removing := p.ActiveCommand != nil && p.ActiveCommand.Kind == projects.CommandRemoveLive
		out.Live = &pb.ProjectLive{DeploymentId: live.DeploymentID, ReleaseId: live.ReleaseID, BuildRevisionId: live.BuildRevisionID, Revision: live.Revision, Url: live.URL, IsRemoving: removing, Version: versionSnapshot(liveVersion)}
	}
	if p.SetupFlowID == "" || p.ProjectStatus == projects.StatusDeleted {
		return out
	}
	switch {
	case summary.Status != "":
		out.Setup = &pb.SetupSnapshot{SetupId: p.SetupID(), Status: boundary.Enum[pb.SetupStatus](setupStatus(summary.Status)), Revision: summary.Revision, Phase: summary.Phase, SourceCommit: summary.SourceCommit, Failure: boundary.FailureSnapshot(summary.Failure)}
	case p.AppManifest == nil:
		out.Setup = &pb.SetupSnapshot{SetupId: p.SetupID(), Status: pb.SetupStatus_SETUP_STATUS_PENDING}
	}
	return out
}

// setupStatus names the closed set a Setup status shown in a snapshot
// belongs to.
func setupStatus(status string) setup.Status { return status }
