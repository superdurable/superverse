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
	"fmt"
	"slices"
	"sync"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/contribution"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// sessionServer serves SessionService: the Project's one Session, its Watch
// with the conversation, workspace and Preview, and the archived history. The
// Project addresses the Session, which its ProjectFlow starts when the Project
// becomes READY.
type sessionServer struct {
	pb.UnimplementedSessionServiceServer
	*boundary.Boundary
	sessions      *agents.SessionFlowService
	sessionAgents *sessionagent.SessionAgentFlowClient
	subagents     *subagent.SubagentFlowClient
}

func (s *sessionServer) GetSession(ctx context.Context, in *pb.GetSessionRequest) (*pb.GetSessionResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	session, err := s.sessions.Get(ctx, account.AccountID, in.GetProjectId())
	if err != nil {
		return nil, boundary.SessionRPCStatus(err)
	}
	return &pb.GetSessionResponse{Session: sessionSnapshot(session)}, nil
}

func (s *sessionServer) WatchSession(in *pb.WatchSessionRequest, stream grpc.ServerStreamingServer[pb.WatchSessionResponse]) error {
	ctx := stream.Context()
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return err
	}
	project := in.GetProjectId()
	// The Session's own Streams never change; the running subagents' come
	// and go with the snapshot that lists them. sources are those of the
	// latest snapshot, none before the first read.
	agentSources := s.agentEventSources(agentloop.SessionFlowID(project))
	var latest sync.Mutex
	var sources []watch.Source[sessionagent.StreamEvent]
	readSnapshot := func(ctx context.Context, accountID string) (*pb.SessionSnapshot, []watch.Source[sessionagent.StreamEvent], error) {
		snapshot, err := s.sessions.Get(ctx, accountID, project)
		if err != nil {
			return nil, nil, err
		}
		read := agentSources
		if snapshot.Agent != nil {
			read = append(slices.Clone(agentSources), s.subagentSources(project, snapshot.Agent.Description.Subagents)...)
		}
		latest.Lock()
		sources = read
		latest.Unlock()
		return sessionSnapshot(snapshot), read, nil
	}
	resource := watch.Resource[*pb.SessionSnapshot, sessionagent.StreamEvent]{
		Connection: s.BindWatch(in, "session/"+account.AccountID+"/"+project),
		// The snapshot's own authorization: the Project scope, one read model
		// query. A Session whose engine execution stopped stays readable.
		Authorize: func(ctx context.Context, accountID string) error {
			return s.sessions.AuthorizeScope(ctx, accountID, project)
		},
		ReadSnapshot: readSnapshot,
		// The first resolution of a connection reads the snapshot, so that its
		// sources hold the running subagents' too and a reconnect while one
		// runs resumes the Session's own Streams from their cursors instead
		// of realigning on fresh tails. Later, the sources of the latest
		// snapshot stand, whose next reread notices a changed set of running
		// subagents; Authorize keeps checking the caller's access.
		ResolveSources: func(ctx context.Context, accountID string) ([]watch.Source[sessionagent.StreamEvent], error) {
			latest.Lock()
			read := sources
			latest.Unlock()
			if read != nil {
				return read, nil
			}
			_, read, err := readSnapshot(ctx, accountID)
			return read, err
		},
		SendSnapshot: func(snapshot *pb.SessionSnapshot, cursor string) error {
			return stream.Send(&pb.WatchSessionResponse{Event: &pb.WatchSessionResponse_Snapshot{Snapshot: snapshot}, ResumeCursor: cursor})
		},
		SendProgress: func(event sessionagent.StreamEvent, cursor string) error {
			return stream.Send(&pb.WatchSessionResponse{Event: &pb.WatchSessionResponse_Progress{Progress: sessionProgress(event)}, ResumeCursor: cursor})
		},
	}
	return boundary.AgentRPCStatus(resource.Run(ctx))
}

// agentEventSources are the three event Streams of one Session Agent.
func (s *sessionServer) agentEventSources(id agentloop.FlowID) []watch.Source[sessionagent.StreamEvent] {
	var sources []watch.Source[sessionagent.StreamEvent]
	for _, stream := range []sessionagent.EventStream{sessionagent.EventStreamReasoning, sessionagent.EventStreamAssistant, sessionagent.EventStreamActivity} {
		sources = append(sources, watch.Source[sessionagent.StreamEvent]{
			Identity: "SessionAgentFlow/" + string(id) + "/" + string(stream),
			Tail: func(ctx context.Context) (string, error) {
				token, err := s.sessionAgents.LatestEventResumeToken(ctx, id, stream)
				return string(token), err
			},
			Read: func(ctx context.Context, cursor string) (sessionagent.StreamEvent, string, error) {
				return watch.ReadUntilMessage(ctx, func(ctx context.Context) (sessionagent.StreamEvent, string, error) {
					event, err := s.sessionAgents.ReadEvent(ctx, id, stream, sessionagent.ResumeToken(cursor))
					return event, string(event.ResumeToken), err
				})
			},
		})
	}
	return sources
}

// subagentSources are the live status lines of the Session's running
// subagents, lost ones aside: the AgentActivity Stream of each, relayed with
// its task ID. They
// are best effort beside the Session's own Streams: a subagent Stream that
// cannot be read, such as one of an execution that already ended, stays
// silent instead of ending the Watch, and the snapshot that no longer lists
// the subagent as running drops it.
func (s *sessionServer) subagentSources(projectID string, records []agenttasks.SubagentRecord) []watch.Source[sessionagent.StreamEvent] {
	var sources []watch.Source[sessionagent.StreamEvent]
	now := time.Now()
	for _, record := range records {
		if record.Status != agenttasks.SubagentRunning || record.IsLost(now) {
			continue
		}
		id := agenttasks.SubagentIdentity{ProjectID: projectID, AgentID: record.AgentID}
		sources = append(sources, watch.Source[sessionagent.StreamEvent]{
			Identity: "SubagentFlow/" + id.FlowID() + "/" + string(sessionagent.EventStreamActivity),
			Tail: func(ctx context.Context) (string, error) {
				token, err := s.subagents.LatestActivityToken(ctx, id)
				if err != nil && ctx.Err() == nil {
					return "", nil
				}
				return token, err
			},
			Read: func(ctx context.Context, cursor string) (sessionagent.StreamEvent, string, error) {
				activity, token, err := watch.ReadUntilMessage(ctx, func(ctx context.Context) (subagent.Activity, string, error) {
					activity, err := s.subagents.ReadActivity(ctx, id, cursor)
					return activity, activity.ResumeToken, err
				})
				if err != nil && ctx.Err() == nil {
					<-ctx.Done()
					return sessionagent.StreamEvent{}, "", ctx.Err()
				}
				return sessionagent.StreamEvent{Kind: sessionagent.StreamEventKindActivity, Activity: activity.Event, ResumeToken: sessionagent.ResumeToken(token),
					CreatedAt: activity.CreatedAt, Source: activity.Source, TaskID: id.TaskID()}, token, err
			},
		})
	}
	return sources
}

// RenewWorkspace keeps the Session's READY workspace running for seven more
// days; renewed is false while the Session is not ACTIVE or its workspace not
// READY.
func (s *sessionServer) RenewWorkspace(ctx context.Context, in *pb.RenewWorkspaceRequest) (*pb.RenewWorkspaceResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	renewed, err := s.sessions.RenewWorkspace(ctx, account.AccountID, in.GetProjectId())
	if err != nil {
		return nil, boundary.AgentRPCStatus(err)
	}
	return &pb.RenewWorkspaceResponse{Renewed: renewed}, nil
}

// ListArchivedMessages pages back through the Agent's archived history from a
// ten-message archive boundary.
func (s *sessionServer) ListArchivedMessages(ctx context.Context, in *pb.ListArchivedMessagesRequest) (*pb.ListArchivedMessagesResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	id, err := s.sessions.AuthorizeSession(ctx, account, in.GetProjectId())
	if err != nil {
		return nil, boundary.AgentRPCStatus(err)
	}
	before, limit, err := archivedMessageRange(in.GetBeforeSequence(), in.GetPageSize())
	if err != nil {
		return nil, err
	}
	page, err := s.sessionAgents.GetArchivedMessageRange(ctx, id.FlowID, before, limit)
	if err != nil {
		return nil, boundary.AgentRPCStatus(err)
	}
	return &pb.ListArchivedMessagesResponse{History: agentHistoryPage(page)}, nil
}

// archivedMessageRange admits a page before a ten-message archive boundary
// with a page size of 10 to 200 in steps of ten, 10 by default.
func archivedMessageRange(before int64, pageSize int32) (conversation.Sequence, int, error) {
	if before <= 10 || (before-1)%10 != 0 {
		return 0, 0, status.Error(codes.InvalidArgument, "before sequence must identify a ten-message archive boundary")
	}
	limit := int(pageSize)
	if limit == 0 {
		limit = 10
	}
	if limit < 10 || limit > 200 || limit%10 != 0 {
		return 0, 0, status.Error(codes.InvalidArgument, "page size must be 10 to 200 in increments of ten")
	}
	return conversation.Sequence(before), limit, nil
}

// connectorContribution maps the record of one connector contribution: its
// branch, its pull request and its status, all public.
func connectorContribution(v contribution.Snapshot) *pb.ConnectorContribution {
	record := v.Record
	out := &pb.ConnectorContribution{Slug: v.Identity.Slug, Module: record.Module, Kind: boundary.Enum[pb.ConnectorContribution_Kind](record.Kind.String()),
		Status: boundary.Enum[pb.ConnectorContribution_Status](record.Status.String()), Branch: record.Branch, HeadCommit: record.HeadCommit,
		Location: boundary.Enum[pb.ConnectorContribution_Location](record.Location.String()), Failure: record.Failure, Description: record.Description}
	if pullRequest := record.PullRequest; pullRequest != nil {
		out.PullRequestNumber, out.PullRequestUrl = pullRequest.Number, pullRequest.URL
	}
	if candidate := record.ReleaseCandidate; candidate != nil {
		out.ReleaseCandidateVersion, out.ReleaseCandidateFailure = candidate.Version, candidate.Failure
		out.ReleaseCandidateStatus = boundary.Enum[pb.ConnectorContribution_ReleaseCandidateStatus](candidate.Status.String())
	}
	return out
}

// sessionSnapshot shows a Session with its conversation, its Sandbox instance
// and its Project's Preview. The Preview is always present: a Project that
// never ran a Preview command reports NOT_STARTED.
func sessionSnapshot(v agents.Snapshot) *pb.SessionSnapshot {
	out := &pb.SessionSnapshot{ProjectId: v.Session.Identity.ProjectID, Status: boundary.Enum[pb.SessionStatus](v.Session.Status), Revision: v.Session.Revision, HeadCommit: v.Session.Identity.HeadCommit, CreatedAt: boundary.OptionalTimestamp(v.Session.CreatedAt), Preview: previewSnapshot(v.Preview())}
	if v.Agent != nil {
		out.Agent = agentSnapshot(*v.Agent)
		if remote := out.Agent.GetDescription().GetExecutor().GetRemote(); remote != nil {
			if v.RemoteLastSeenAt != nil {
				remote.LastSeenAt = boundary.OptionalTimestamp(*v.RemoteLastSeenAt)
			}
		}
	}
	for _, contribution := range v.Contributions {
		out.ConnectorContributions = append(out.ConnectorContributions, connectorContribution(contribution))
	}
	updated := v.Session.UpdatedAt
	if v.Workspace != nil && v.Workspace.Instance.State != sessionworkspace.InstanceNone {
		out.Workspace = workspaceSnapshot(v.Session, v.Workspace.Instance)
		if v.Workspace.Instance.UpdatedAt.After(updated) {
			updated = v.Workspace.Instance.UpdatedAt
		}
	}
	out.UpdatedAt = boundary.OptionalTimestamp(updated)
	if v.ExecutionFailure != agentloop.ExecutionRunning {
		out.Status = pb.SessionStatus_SESSION_STATUS_FAILED
		out.Failure = &pb.Failure{Code: v.ExecutionFailure.String(), Phase: "agent", Message: "The Agent execution stopped. Its conversation and last workspace state remain available.", IsRecoverable: true,
			Remedy: pb.FailureRemedy_FAILURE_REMEDY_CONTACT_OPERATOR, Owner: pb.FailureOwner_FAILURE_OWNER_PLATFORM}
	}
	return out
}

// workspaceSnapshot shows the Session's Sandbox instance, which the
// SessionWorkspaceFlow owns: its state as the API's workspace status, the
// constant generation, the Session's revision, what the volume holds, the
// Sandbox deadline of a READY request, which the Session owns, the phases of
// its provisioning and the failure of the Session's last request, else of the
// instance's last reclamation.
func workspaceSnapshot(session agentloop.SessionSummary, instance sessionworkspace.Instance) *pb.SessionWorkspaceSnapshot {
	w := session.Workspace
	out := &pb.SessionWorkspaceSnapshot{Status: boundary.Enum[pb.WorkspaceStatus](instance.State.WorkspaceStatus()), Generation: sandbox.InstanceGeneration, Revision: session.Revision,
		WorkspaceState: boundary.Enum[pb.WorkspaceState](instance.VolumeState), UpdatedAt: boundary.OptionalTimestamp(instance.UpdatedAt),
		ExpiresAt: boundary.OptionalTimestamp(w.SandboxDeadline), WorkspaceRequestId: instance.InstanceID, PhaseTimings: boundary.PhaseTimingSnapshots(instance.Timings)}
	switch {
	case w.Failure != "":
		out.Failure = &pb.Failure{Code: w.Failure, Phase: "workspace", Message: w.FailureDetail, IsRecoverable: true, Remedy: pb.FailureRemedy_FAILURE_REMEDY_APPLY_AGAIN, Owner: pb.FailureOwner_FAILURE_OWNER_PLATFORM}
	case instance.State == sessionworkspace.InstanceFailed && instance.Failure != nil:
		out.Failure = &pb.Failure{Code: instance.Failure.Code, Phase: "workspace", Message: instance.Failure.Detail, IsRecoverable: true, Remedy: pb.FailureRemedy_FAILURE_REMEDY_APPLY_AGAIN, Owner: pb.FailureOwner_FAILURE_OWNER_PLATFORM}
	}
	return out
}

// sessionProgress is one event of the Session Agent's reasoning, assistant or
// activity Stream.
func sessionProgress(e sessionagent.StreamEvent) *pb.SessionProgress {
	out := &pb.SessionProgress{CreatedAt: timestamppb.New(e.CreatedAt), Source: e.Source, TaskId: e.TaskID}
	switch e.Kind {
	case sessionagent.StreamEventKindReasoning:
		out.Content = &pb.SessionProgress_ReasoningSummary{ReasoningSummary: e.Text}
	case sessionagent.StreamEventKindAssistant:
		out.Content = &pb.SessionProgress_AssistantText{AssistantText: e.Text}
	case sessionagent.StreamEventKindActivity:
		out.Content = &pb.SessionProgress_Activity{Activity: agentActivity(e.Activity)}
	default:
		panic(fmt.Sprintf("unknown Session event kind %q", e.Kind))
	}
	return out
}
