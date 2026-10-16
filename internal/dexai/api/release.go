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
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// releaseServer serves ReleaseService. A Release is one main commit of a
// Project, and its ID is the commit SHA. PrepareRelease reads its source; each
// build revision then builds it with one frozen configuration.
type releaseServer struct {
	pb.UnimplementedReleaseServiceServer
	*boundary.Boundary
	projects *projects.ProjectFlowClient
	host     repohost.Host
	releases *releases.ProjectReleaseFlowClient
	// configuration edits the Live draft of each Release's version.
	configuration *projectconfig.Configuration
	// publishing reads the redacted build logs.
	publishing *deployments.PublishingFlowService
	// versions caches the versions of the listed release candidates.
	versions commitVersions
}

// ListReleaseCandidates lists the recent commits of the Project's main branch.
func (s *releaseServer) ListReleaseCandidates(ctx context.Context, in *pb.ListReleaseCandidatesRequest) (*pb.ListReleaseCandidatesResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	projection, err := s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityBuild)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	binding := projection.Project.RepositoryBinding
	if binding.RepositoryID == 0 {
		return nil, status.Error(codes.FailedPrecondition, "project repository is unavailable")
	}
	commits, err := s.host.ListCommits(ctx, binding.FullName, "main")
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	versions := make([]repohost.CommitVersion, len(commits))
	group, described := errgroup.WithContext(ctx)
	group.SetLimit(maximumVersionReads)
	for index, c := range commits {
		group.Go(func() (err error) {
			versions[index], err = s.versions.describe(described, s.host, binding.FullName, c.SHA)
			return err
		})
	}
	if err = group.Wait(); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListReleaseCandidatesResponse{}
	for index, c := range commits {
		subject, _, _ := strings.Cut(c.Message, "\n")
		out.Candidates = append(out.Candidates, &pb.ReleaseCandidate{SourceCommit: c.SHA, Subject: subject, CommittedAt: timestamppb.New(c.CommittedAt), Version: versionSnapshot(versions[index])})
	}
	return out, nil
}

const (
	// maximumVersionReads bounds the concurrent version reads of one listing.
	maximumVersionReads = 8
	// maximumCachedVersions bounds the commit versions kept in process.
	maximumCachedVersions = 1024
)

// commitVersions keeps the versions of commits that release candidate
// listings described, by commit SHA, at most maximumCachedVersions, the
// oldest dropped first. A commit's version never changes, and a SHA names its
// whole history, so the same SHA in two repositories has the same version.
type commitVersions struct {
	mu       sync.Mutex
	versions map[string]repohost.CommitVersion
	order    []string
}

// describe returns the version of commit, from the cache or the host.
func (c *commitVersions) describe(ctx context.Context, host repohost.Host, fullName, commit string) (repohost.CommitVersion, error) {
	c.mu.Lock()
	version, cached := c.versions[commit]
	c.mu.Unlock()
	if cached {
		return version, nil
	}
	version, err := host.DescribeCommit(ctx, fullName, commit)
	if err != nil {
		return repohost.CommitVersion{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, cached = c.versions[commit]; !cached {
		if c.versions == nil {
			c.versions = map[string]repohost.CommitVersion{}
		}
		if len(c.order) == maximumCachedVersions {
			delete(c.versions, c.order[0])
			c.order = c.order[1:]
		}
		c.versions[commit] = version
		c.order = append(c.order, commit)
	}
	return version, nil
}

func (s *releaseServer) PrepareRelease(ctx context.Context, in *pb.PrepareReleaseRequest) (*pb.PrepareReleaseResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	release, err := s.releases.Prepare(ctx, account.AccountID, in.GetProjectId(), in.GetRequestId(), in.GetSourceCommit())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.PrepareReleaseResponse{ReleaseId: release}, nil
}

func (s *releaseServer) StartReleaseBuild(ctx context.Context, in *pb.StartReleaseBuildRequest) (*pb.StartReleaseBuildResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	// A reason of only spaces is no reason.
	reason := strings.TrimSpace(in.GetPreviewSkipReason())
	if err = boundary.InvalidIf(utf8.RuneCountInString(reason) > releases.MaximumSkipReason, "preview_skip_reason is longer than 500 characters"); err != nil {
		return nil, err
	}
	request := releases.BuildRevisionRequest{RequestID: in.GetRequestId(), BuildRevisionID: in.GetBuildRevisionId(), ExpectedRevision: in.GetExpectedRevision(), ConfigurationRevision: in.GetConfigurationRevision(), AutoDeploy: in.GetAutoDeploy(), ExpectedLiveRevision: in.GetExpectedLiveRevision(), PreviewSkipReason: reason}
	build, err := s.releases.BuildRevision(ctx, account.AccountID, in.GetProjectId(), in.GetReleaseId(), request)
	if err != nil {
		return nil, boundary.ReleaseBuildRPCStatus(err)
	}
	return &pb.StartReleaseBuildResponse{BuildRevisionId: build}, nil
}

func (s *releaseServer) RecoverReleaseBuild(ctx context.Context, in *pb.RecoverReleaseBuildRequest) (*pb.RecoverReleaseBuildResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	recovery := releases.BuildRevisionRecovery{RequestID: in.GetRequestId(), BuildRevisionID: in.GetBuildRevisionId(), ExpectedRevision: in.GetExpectedRevision()}
	if err = s.releases.RecoverBuildRevision(ctx, account.AccountID, in.GetProjectId(), in.GetReleaseId(), recovery); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.RecoverReleaseBuildResponse{}, nil
}

// CancelReleaseBuild cancels the Release's active build until its artifacts
// commit; a build whose automatic deployment may be admitted is refused with
// the deployment to cancel instead.
func (s *releaseServer) CancelReleaseBuild(ctx context.Context, in *pb.CancelReleaseBuildRequest) (*pb.CancelReleaseBuildResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	cancellation := releases.BuildRevisionCancellation{BuildRevisionID: in.GetBuildRevisionId(), RequestID: in.GetRequestId()}
	if err = s.releases.CancelBuildRevision(ctx, account.AccountID, in.GetProjectId(), in.GetReleaseId(), cancellation); err != nil {
		return nil, boundary.CancellationRPCStatus(err)
	}
	return &pb.CancelReleaseBuildResponse{}, nil
}

// maximumLogCursorBytes bounds a build log cursor, which only a page returned.
const maximumLogCursorBytes = 1024

// GetReleaseBuildLog reads one redacted page of a build revision's provider
// log for a builder of the Project; nothing is stored. Under the server's
// shared log read budget the page is empty and retry_after_ms says when to
// read again.
func (s *releaseServer) GetReleaseBuildLog(ctx context.Context, in *pb.GetReleaseBuildLogRequest) (*pb.GetReleaseBuildLogResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.InvalidIf(len(in.GetCursor()) > maximumLogCursorBytes, "cursor is longer than 1024 bytes"); err != nil {
		return nil, err
	}
	projection, err := s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityBuild)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	if !build.RevisionIDPattern.MatchString(in.GetBuildRevisionId()) {
		return nil, status.Error(codes.InvalidArgument, "build revision requires a build-UUIDv4 identity")
	}
	page, err := s.publishing.ReadBuildLog(ctx, projection.Project, in.GetReleaseId(), in.GetBuildRevisionId(), build.LogRead{Cursor: in.GetCursor(), MaximumBytes: deployments.MaximumBuildLogPageBytes})
	if err != nil {
		return nil, boundary.CancellationRPCStatus(err)
	}
	return &pb.GetReleaseBuildLogResponse{Lines: page.Lines(), NextCursor: page.Next, Complete: page.Complete, RetryAfterMs: uint32(page.RetryAfter.Milliseconds())}, nil //nolint:gosec // at most a minute
}

func (s *releaseServer) ListReleases(ctx context.Context, in *pb.ListReleasesRequest) (*pb.ListReleasesResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	statuses, err := boundary.DomainValues(in.GetStatuses())
	if err != nil {
		return nil, err
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityBuild); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	page, err := s.ReadPage("releases/"+account.AccountID+"/"+in.GetProjectId()+"/"+strings.Join(statuses, ","), in)
	if err != nil {
		return nil, err
	}
	listed, next, err := s.releases.ListProjections(ctx, in.GetProjectId(), statuses, page.After, page.Size)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListReleasesResponse{NextPageToken: s.NextPageToken(page, next)}
	for _, projection := range listed {
		out.Releases = append(out.Releases, releaseSnapshot(projection))
	}
	return out, nil
}

// GetRelease keeps unfinished and failed Releases readable without artifacts.
// Other prepared Releases carry their exact immutable source definitions or
// fail. A read timeout says nothing about the durable build outcome.
func (s *releaseServer) GetRelease(ctx context.Context, in *pb.GetReleaseRequest) (*pb.GetReleaseResponse, error) {
	started := time.Now()
	account, err := s.Authenticate(ctx, in.GetCaller())
	s.observeReleaseRead(ctx, in.GetProjectId(), in.GetReleaseId(), releaseReadAuthentication, started, err)
	if err != nil {
		// Authenticate already maps its error at the caller trust boundary.
		if status.Code(err) == codes.DeadlineExceeded {
			return nil, boundary.ReleaseReadStatus(context.DeadlineExceeded)
		}
		return nil, err
	}
	started = time.Now()
	_, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityView)
	s.observeReleaseRead(ctx, in.GetProjectId(), in.GetReleaseId(), releaseReadAuthorization, started, err)
	if err != nil {
		return nil, boundary.ReleaseReadStatus(err)
	}
	started = time.Now()
	release, err := s.releases.ReadProjection(ctx, in.GetProjectId(), in.GetReleaseId())
	s.observeReleaseRead(ctx, in.GetProjectId(), in.GetReleaseId(), releaseReadSnapshot, started, err)
	if err != nil {
		return nil, boundary.ReleaseReadStatus(err)
	}
	started = time.Now()
	definitions, err := s.releases.PreparedDefinitions(ctx, release.Release)
	s.observeReleaseRead(ctx, in.GetProjectId(), in.GetReleaseId(), releaseReadDefinitions, started, err)
	if err != nil {
		return nil, boundary.ReleaseReadStatus(err)
	}
	return &pb.GetReleaseResponse{Release: releaseSnapshot(release), FlowDefinitions: boundary.FlowDefinitionSnapshots(definitions)}, nil
}

type releaseReadPhase string

const (
	releaseReadAuthentication releaseReadPhase = "authentication"
	releaseReadAuthorization  releaseReadPhase = "project_authorization"
	releaseReadSnapshot       releaseReadPhase = "release_snapshot"
	releaseReadDefinitions    releaseReadPhase = "prepared_definitions"
)

// observeReleaseRead keeps slow successful reads as well as failures visible,
// without logging caller credentials, snapshots or object contents. Normal
// sub-second reads stay at Debug; this introduces no durable state or retry.
func (s *releaseServer) observeReleaseRead(ctx context.Context, projectID, releaseID string, phase releaseReadPhase, started time.Time, err error) {
	elapsed := time.Since(started)
	level := slog.LevelDebug
	if elapsed >= time.Second {
		level = slog.LevelInfo
	}
	attrs := []any{"project_id", projectID, "release_id", releaseID, "phase", phase, "duration", elapsed, "failed", err != nil}
	if err != nil {
		level = slog.LevelWarn
		attrs = append(attrs, "read_status", status.Code(boundary.ReleaseReadStatus(err)).String())
		attrs = append(attrs, boundary.DexErrorAttrs(err)...)
	}
	s.Logger.Log(ctx, level, "Release state read", attrs...)
}

func (s *releaseServer) WatchRelease(in *pb.WatchReleaseRequest, stream grpc.ServerStreamingServer[pb.WatchReleaseResponse]) error {
	ctx := stream.Context()
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return err
	}
	flowID, err := releases.FlowID(in.GetProjectId(), in.GetReleaseId())
	if err != nil {
		return boundary.RPCStatus(err)
	}
	sources := []watch.Source[dexflow.ProgressEvent]{watch.DexStreamSource(s.Dex, flowID, releases.BuildProgress)}
	resource := watch.Resource[*pb.ReleaseSnapshot, dexflow.ProgressEvent]{
		Connection: s.BindWatch(in, account.AccountID+"/"+flowID),
		Authorize: func(ctx context.Context, accountID string) error {
			_, err := s.projects.Authorize(ctx, accountID, in.GetProjectId(), projects.CapabilityBuild)
			return err
		},
		ReadSnapshot: func(ctx context.Context, accountID string) (*pb.ReleaseSnapshot, []watch.Source[dexflow.ProgressEvent], error) {
			if _, err := s.projects.Authorize(ctx, accountID, in.GetProjectId(), projects.CapabilityBuild); err != nil {
				return nil, nil, err
			}
			release, err := s.releases.ReadProjection(ctx, in.GetProjectId(), in.GetReleaseId())
			if err != nil {
				return nil, nil, err
			}
			return releaseSnapshot(release), sources, nil
		},
		SendSnapshot: func(snapshot *pb.ReleaseSnapshot, cursor string) error {
			return stream.Send(&pb.WatchReleaseResponse{Event: &pb.WatchReleaseResponse_Snapshot{Snapshot: snapshot}, ResumeCursor: cursor})
		},
		SendProgress: func(event dexflow.ProgressEvent, cursor string) error {
			return stream.Send(&pb.WatchReleaseResponse{Event: &pb.WatchReleaseResponse_Progress{Progress: resourceProgress(event)}, ResumeCursor: cursor})
		},
	}
	return boundary.RPCStatus(resource.Run(ctx))
}

// releaseSnapshot shows a Release with its active and ready build revisions.
func releaseSnapshot(p releases.ReleaseProjection) *pb.ReleaseSnapshot {
	r, source := p.Release, p.Release.PreparedSource
	out := &pb.ReleaseSnapshot{ActiveBuildRevisionId: r.ActiveBuildRevisionID, ReadyBuildRevisionId: r.ReadyBuildRevisionID, ProjectId: r.ProjectID, ReleaseId: r.ReleaseID, Status: boundary.Enum[pb.ReleaseStatus](r.Status), Revision: r.Revision, SourceCommit: r.SourceCommit, BuildProfileVersion: r.BuildProfileVersion, Failure: boundary.FailureSnapshot(r.Failure), CreatedAt: timestamppb.New(r.CreatedAt),
		PreparedSource: &pb.PreparedReleaseSourceSnapshot{Status: boundary.Enum[pb.ReleaseStatus](source.Status), SourceCommit: source.SourceCommit, DefinitionRevision: source.DefinitionRevision, AppManifest: source.AppManifest, Failure: boundary.OwnedFailureSnapshot(source.Failure, source.FailureOwner), PhaseTimings: boundary.PhaseTimingSnapshots(source.Timings), Version: versionSnapshot(source.Version)}}
	for _, build := range p.Builds {
		frozen := uint64(0)
		if build.Configuration != nil {
			frozen = build.Configuration.ConfigurationRevision
		}
		out.BuildRevisions = append(out.BuildRevisions, &pb.ReleaseBuildRevisionSnapshot{ProviderSettled: build.ProviderSettled, BuildRevisionId: build.BuildRevisionID, Status: boundary.Enum[pb.ReleaseStatus](build.Status), Revision: build.Revision, ConfigurationRevision: frozen, DraftConfigurationRevision: build.ConfigurationRevision, ImageDigest: build.ImageDigest, BuildProfileVersion: build.BuildProfileVersion, Failure: boundary.OwnedFailureSnapshot(build.Failure, build.FailureOwner), AutoDeploy: build.AutoDeploy, AutoDeployStatus: boundary.Enum[pb.AutoDeployStatus](build.AutoDeployStatus), AutoDeploymentId: build.AutoDeploymentID, AutoDeployFailure: boundary.OwnedFailureSnapshot(build.AutoDeployFailure, build.FailureOwner), CreatedAt: timestamppb.New(build.CreatedAt), PhaseTimings: boundary.PhaseTimingSnapshots(build.Timings), PreviewValidation: previewValidationSnapshot(build.PreviewValidation),
			Cancellation: buildCancellationSnapshot(build.Cancellation)})
	}
	return out
}

// buildCancellationSnapshot is who cancelled a build and when, or nil.
func buildCancellationSnapshot(c *releases.BuildCancellation) *pb.Cancellation {
	if c == nil {
		return nil
	}
	return &pb.Cancellation{ActorAccountId: c.ActorAccountID, RequestedAt: timestamppb.New(c.RequestedAt)}
}

// previewValidationSnapshot is how a build met the publishing rule, or nil
// for a build admitted before it was recorded.
func previewValidationSnapshot(v *releases.PreviewValidation) *pb.PreviewValidation {
	if v == nil {
		return nil
	}
	out := &pb.PreviewValidation{Kind: boundary.Enum[pb.PreviewValidation_Kind](v.Kind.String()), StartCommandId: v.StartCommandID, SkipReason: v.SkipReason, ActorAccountId: v.ActorAccountID}
	if !v.StartedAt.IsZero() {
		out.StartedAt = timestamppb.New(v.StartedAt)
	}
	return out
}

// DeriveLiveConfiguration builds the frozen copy of a Release's version from
// Live's next-publish layer at the revisions the caller showed, which a build
// of the version then freezes.
func (s *releaseServer) DeriveLiveConfiguration(ctx context.Context, in *pb.DeriveLiveConfigurationRequest) (*pb.DeriveLiveConfigurationResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if _, err = s.configuration.DeriveLive(ctx, account.AccountID, in.GetProjectId(), in.GetReleaseId(), in.GetExpectedRevision(), in.GetExpectedPreviewConfigurationRevision()); err != nil {
		return nil, boundary.ConfigurationRPCStatus(err)
	}
	configuration, err := configurationWithItems(ctx, s.configuration, account.AccountID, liveTarget(in.GetProjectId(), in.GetReleaseId()))
	if err != nil {
		return nil, boundary.ConfigurationRPCStatus(err)
	}
	return &pb.DeriveLiveConfigurationResponse{Configuration: configuration}, nil
}

func liveTarget(projectID, releaseID string) *pb.ProjectConfigurationTarget {
	return &pb.ProjectConfigurationTarget{ProjectId: projectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, ReleaseId: releaseID}
}

// configurationWithItems is the configuration target addresses with its
// items: for Preview compared with the applied version, with the count an
// Apply would change; for Live its next publish as it resolves for the
// target's source, with whether a Release's frozen copy is stale.
func configurationWithItems(ctx context.Context, configuration *projectconfig.Configuration, actor string, target *pb.ProjectConfigurationTarget) (*pb.ProjectConfigurationSnapshot, error) {
	snapshot, items, err := configuration.Get(ctx, actor, target)
	if err != nil {
		return nil, err
	}
	if target.GetScope() == pb.ProjectScope_PROJECT_SCOPE_PREVIEW {
		preview, previewErr := configuration.PreviewItems(ctx, actor, target.GetProjectId())
		if previewErr != nil {
			return nil, previewErr
		}
		items, snapshot.UnappliedChangeCount = preview.Items, uint32(preview.UnappliedChangeCount) //nolint:gosec // a configuration holds a bounded number of items
	}
	for _, item := range items {
		warnings := make([]pb.ConfigurationItemWarning, 0, len(item.Warnings))
		for _, warning := range item.Warnings {
			warnings = append(warnings, boundary.Enum[pb.ConfigurationItemWarning](warning))
		}
		snapshot.Items = append(snapshot.Items, &pb.ConfigurationItem{
			Key: &pb.ConfigurationItemKey{
				Kind: boundary.Enum[pb.ConfigurationItemKey_Kind](item.Kind), ConnectorId: item.ConnectorID, ConnectionName: item.ConnectionName, TriggerName: item.TriggerName,
				BindingName: item.BindingName, OperationId: item.OperationID, FlowType: item.FlowType, StepType: item.StepType, EnvironmentName: item.EnvironmentName,
			},
			State: boundary.Enum[pb.ConfigurationItem_State](item.State), DiffersFromLive: item.DiffersFromLive, DiffersFromPreview: item.DiffersFromPreview,
			Warnings: warnings, WarningProjectName: item.WarningProjectName, LiveValueJson: string(item.LiveValueJSON), PreviewValueJson: string(item.PreviewValueJSON),
			FollowRefusal: item.FollowRefusal.Wire(),
		})
	}
	return snapshot, nil
}
