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

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/platform/repohost"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// previewServer serves PreviewService. A Project's Preview serves its
// workspace's application on the current Sandbox instance of the Project's
// Session, which the Preview acquires from the Session; the Session Watch
// carries its progress.
type previewServer struct {
	pb.UnimplementedPreviewServiceServer
	*boundary.Boundary
	sessions *agents.SessionFlowService
}

// previewCommandRequest is a Preview command with its command ID and the
// Preview it names: the Preview's revision, its generation and the Sandbox
// instance it uses, as the caller observed them.
type previewCommandRequest interface {
	GetCaller() *pb.CallerContext
	GetProjectId() string
	GetExpectedRevision() uint64
	GetGeneration() uint64
	GetWorkspaceRequestId() string
	GetRequestId() string
}

// validateObservation rejects an incomplete observation. A command names the
// Preview's revision and generation with the instance the Preview acquired,
// which is empty while it never acquired one; or it names nothing, in the
// first-demand form that only a Project without a Preview admits.
func validateObservation(in previewCommandRequest) error {
	observed := in.GetGeneration() != 0 && in.GetExpectedRevision() != 0
	firstDemand := in.GetWorkspaceRequestId() == "" && in.GetGeneration() == 0 && in.GetExpectedRevision() == 0
	return boundary.InvalidIf(!observed && !firstDemand, "Preview requires the observed revision and generation, with the workspace request ID once the Preview acquired an instance")
}

// previewCommandOf is the Preview command of kind that actor sends on the
// Preview the caller observed, once its input passes the checks.
func previewCommandOf(in previewCommandRequest, kind preview.CommandKind, actor string) (preview.Command, error) {
	if err := boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return preview.Command{}, err
	}
	if err := validateObservation(in); err != nil {
		return preview.Command{}, err
	}
	return preview.Command{ID: in.GetRequestId(), Kind: kind, WorkspaceRequestID: in.GetWorkspaceRequestId(), Generation: in.GetGeneration(), ExpectedRevision: in.GetExpectedRevision(),
		ActorAccountID: actor}, nil
}

// previewCommand admits one Preview command on the Preview the caller observed
// and returns the Preview after it. Only a start applies configuration: at
// exactly the revision the caller saw, which it names when it applies.
func (s *previewServer) previewCommand(ctx context.Context, in previewCommandRequest, kind preview.CommandKind) (*pb.PreviewSnapshot, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	command, err := previewCommandOf(in, kind, account.AccountID)
	if err != nil {
		return nil, err
	}
	if start, isStart := in.(*pb.StartPreviewRequest); isStart {
		applying := start.GetApplyConfiguration()
		if err = boundary.InvalidIf(applying != (start.GetConfigurationRevision() > 0), "configuration_revision is required exactly when the start applies configuration"); err != nil {
			return nil, err
		}
		command.ApplyConfiguration, command.ConfigurationRevision = applying, start.GetConfigurationRevision()
	}
	state, err := s.sessions.PreviewCommand(ctx, account.AccountID, in.GetProjectId(), command)
	if err != nil {
		return nil, boundary.SessionRPCStatus(err)
	}
	return previewSnapshot(state), nil
}

func (s *previewServer) GetPreview(ctx context.Context, in *pb.GetPreviewRequest) (*pb.GetPreviewResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	state, err := s.sessions.GetPreview(ctx, account.AccountID, in.GetProjectId())
	if err != nil {
		return nil, boundary.SessionRPCStatus(err)
	}
	return &pb.GetPreviewResponse{Preview: previewSnapshot(state)}, nil
}

func (s *previewServer) StartPreview(ctx context.Context, in *pb.StartPreviewRequest) (*pb.StartPreviewResponse, error) {
	state, err := s.previewCommand(ctx, in, preview.CommandStartPreview)
	if err != nil {
		return nil, err
	}
	return &pb.StartPreviewResponse{Preview: state}, nil
}

func (s *previewServer) StopPreview(ctx context.Context, in *pb.StopPreviewRequest) (*pb.StopPreviewResponse, error) {
	state, err := s.previewCommand(ctx, in, preview.CommandStopPreview)
	if err != nil {
		return nil, err
	}
	return &pb.StopPreviewResponse{Preview: state}, nil
}

// PreparePreviewSource returns acceptance only; the Session Watch carries the
// preparation's progress and outcome.
func (s *previewServer) PreparePreviewSource(ctx context.Context, in *pb.PreparePreviewSourceRequest) (*pb.PreparePreviewSourceResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	command, err := previewCommandOf(in, preview.CommandPreparePreviewSource, account.AccountID)
	if err != nil {
		return nil, err
	}
	if err = s.sessions.PreparePreviewSource(ctx, account.AccountID, in.GetProjectId(), command); err != nil {
		return nil, boundary.SessionRPCStatus(err)
	}
	return &pb.PreparePreviewSourceResponse{}, nil
}

func (s *previewServer) GetPreviewDefinitions(ctx context.Context, in *pb.GetPreviewDefinitionsRequest) (*pb.GetPreviewDefinitionsResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	state, definitions, err := s.sessions.GetPreviewDefinitions(ctx, account.AccountID, in.GetProjectId())
	if err != nil {
		return nil, boundary.SessionRPCStatus(err)
	}
	return &pb.GetPreviewDefinitionsResponse{Preview: previewSnapshot(state), FlowDefinitions: boundary.FlowDefinitionSnapshots(definitions)}, nil
}

// UpdatePreviewAutoDeploy turns the Preview's deployment of each pushed commit
// on or off for the Project's Session, through the Session Service. It maps
// its errors like an Agent command: a Session that is not ACTIVE answers
// FAILED_PRECONDITION, and a stopped engine execution its SESSION_EXECUTION_*
// reason.
func (s *previewServer) UpdatePreviewAutoDeploy(ctx context.Context, in *pb.UpdatePreviewAutoDeployRequest) (*pb.UpdatePreviewAutoDeployResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.InvalidIf(in.IsEnabled == nil, "is_enabled is required"); err != nil {
		return nil, err
	}
	if err = s.sessions.UpdatePreviewAutoDeploy(ctx, account, in.GetProjectId(), in.GetIsEnabled()); err != nil {
		return nil, boundary.AgentRPCStatus(err)
	}
	return &pb.UpdatePreviewAutoDeployResponse{}, nil
}

// previewSnapshot shows the Project's Preview with the source prepared for its
// instance, its automatic deployment, the configuration gaps of its last
// refused start and the phases of its last start. Revision is the Preview's
// command fence.
func previewSnapshot(v preview.Snapshot) *pb.PreviewSnapshot {
	source := v.PreparedSource
	out := &pb.PreviewSnapshot{ProjectId: v.Identity.ProjectID, Status: boundary.Enum[pb.PreviewStatus](v.Preview.Status), Revision: v.Revision, Generation: v.Generation, Url: v.Preview.URL, WorkspaceRequestId: v.WorkspaceRequestID, SourceCommit: v.Preview.SourceCommit, DefinitionRevision: v.Preview.DefinitionRevision, ServingRevision: v.Preview.Revision, Failure: boundary.OwnedFailureSnapshot(v.Failure, v.FailureOwner),
		PreparedSource:      &pb.PreparedPreviewSourceSnapshot{Status: boundary.Enum[pb.PreviewSourceStatus](source.Status), RequestId: source.RequestID, WorkspaceRequestId: source.WorkspaceRequestID, Generation: source.Generation, SourceCommit: source.SourceCommit, DefinitionRevision: source.DefinitionRevision, Failure: boundary.FailureSnapshot(source.Failure)},
		AutomaticDeployment: previewDeploymentSnapshot(v.Deployment), ConfigurationGaps: appmanifest.RequirementGapSnapshots(v.Preview.ConfigurationGaps),
		PhaseTimings: boundary.PhaseTimingSnapshots(v.Preview.Timings), ServingVersion: versionSnapshot(v.Preview.Version)}
	out.PreparedSource.Version = versionSnapshot(source.Version)
	if ref := source.Definitions; ref.Key != "" {
		out.PreparedSource.Definitions = &pb.ObjectReference{Key: ref.Key, Version: ref.Version, Digest: ref.Digest, MediaType: ref.MediaType}
	}
	if applied := v.Applied; applied.ObjectKey != "" {
		out.AppliedConfiguration = &pb.AppliedPreviewConfiguration{ConfigurationRevision: applied.ConfigurationRevision, SourceCommit: applied.SourceCommit,
			AppliedAt: timestamppb.New(applied.AppliedAt), AppliedByAccountId: applied.ActorAccountID}
	}
	for _, record := range v.RecentStarts {
		out.RecentStarts = append(out.RecentStarts, &pb.PreviewStartRecord{CommandId: record.CommandID, ActorAccountId: record.ActorAccountID, SourceCommit: record.Commit,
			ConfigurationRevision: record.ConfigurationRevision, Applied: record.Applied, AdmittedAt: timestamppb.New(record.AdmittedAt), EndedAt: boundary.OptionalTimestamp(record.EndedAt),
			Outcome: boundary.Enum[pb.StartOutcome](record.Outcome.String()), FailureCode: record.FailureCode, PhaseTimings: boundary.PhaseTimingSnapshots(record.Timings),
			Version: versionSnapshot(record.Version)})
	}
	return out
}

// versionSnapshot is a recorded version, or nil for one not recorded.
func versionSnapshot(version repohost.CommitVersion) *pb.CommitVersion {
	if version == (repohost.CommitVersion{}) {
		return nil
	}
	return boundary.CommitVersionSnapshot(&version)
}

// previewDeploymentSnapshot shows the newest deployment the Preview handled and
// the commit of the request that waits, or nil before the first request.
func previewDeploymentSnapshot(deployment preview.PreviewDeployment) *pb.PreviewDeploymentSnapshot {
	last, queued := deployment.Last, deployment.Queued
	if last.CommandID == "" && queued == nil {
		return nil
	}
	out := &pb.PreviewDeploymentSnapshot{Commit: last.Commit, Status: boundary.Enum[pb.PreviewDeploymentStatus](last.Status.String()), Failure: boundary.OwnedFailureSnapshot(last.Failure, last.Owner)}
	if !last.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(last.UpdatedAt)
	}
	if queued != nil {
		out.QueuedCommit = queued.Commit
	}
	return out
}
