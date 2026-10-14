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
	"slices"
	"strings"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/tenancy/slackrelay"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// environmentEventKind is a kind of EnvironmentEvent, the name of the
// protobuf value without its prefix.
type environmentEventKind string

const (
	eventPreviewConfigurationApplied environmentEventKind = "PREVIEW_CONFIGURATION_APPLIED"
	eventPreviewStarted              environmentEventKind = "PREVIEW_STARTED"
	eventPublished                   environmentEventKind = "PUBLISHED"
	eventDeployedExistingVersion     environmentEventKind = "DEPLOYED_EXISTING_VERSION"
	eventPublishCancelled            environmentEventKind = "PUBLISH_CANCELLED"
)

// maximumEnvironmentEvents is the page size a timeline accepts.
const maximumEnvironmentEvents = 50

// formerMember is how the timeline names an actor who left the TeamSpace.
const formerMember = "a former member"

// eventsPageRequest reads the page size of ListEnvironmentEvents, which the
// protocol declares unsigned, as the paging of every List RPC.
type eventsPageRequest struct {
	*pb.ListEnvironmentEventsRequest
}

func (r eventsPageRequest) GetPageSize() int32 {
	return int32(r.ListEnvironmentEventsRequest.GetPageSize())
}

// ListEnvironmentEvents returns the newest-first history of one environment,
// derived from existing state alone. Preview needs the builder capability and
// lists the starts its Preview recorded, at most 50 and the only history it
// keeps. Live needs the operator capability and lists the Release and
// Deployment read model by creation time.
func (s *projectServer) ListEnvironmentEvents(ctx context.Context, in *pb.ListEnvironmentEventsRequest) (*pb.ListEnvironmentEventsResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	capability := projects.CapabilityOperateLive
	switch in.GetScope() {
	case pb.ProjectScope_PROJECT_SCOPE_PREVIEW:
		capability = projects.CapabilityBuild
	case pb.ProjectScope_PROJECT_SCOPE_LIVE:
	default:
		return nil, boundary.InvalidIf(true, "scope must be Preview or Live")
	}
	if err = boundary.InvalidIf(in.GetPageSize() > maximumEnvironmentEvents, "page size must be at most 50"); err != nil {
		return nil, err
	}
	projection, err := s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), capability)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	page, err := s.ReadPage("events/"+account.AccountID+"/"+in.GetProjectId()+"/"+in.GetScope().String(), eventsPageRequest{in})
	if err != nil {
		return nil, err
	}
	var events []*timelineEvent
	var next *postgres.PageKey
	if in.GetScope() == pb.ProjectScope_PROJECT_SCOPE_PREVIEW {
		events, next, err = s.previewEvents(ctx, in.GetProjectId(), page)
	} else {
		events, next, err = s.liveEvents(ctx, in.GetProjectId(), page)
	}
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	if err = s.nameActors(ctx, projection.Project.TeamSpaceID, events); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListEnvironmentEventsResponse{NextPageToken: s.NextPageToken(page, next)}
	for _, event := range events {
		out.Events = append(out.Events, event.event)
	}
	return out, nil
}

// timelineEvent is an event with the position it is paged by.
type timelineEvent struct {
	id    string
	event *pb.EnvironmentEvent
}

func (e *timelineEvent) key() postgres.PageKey {
	return postgres.PageKey{CreatedAt: e.event.GetOccurredAt().AsTime(), ID: e.id}
}

// previewEvents derives the events of the Preview's recorded starts. A start
// that applied configuration is an Apply. One event holds the start with its
// outcome, failure and phases, as its record does.
// The Agent's push has no actor.
func (s *projectServer) previewEvents(ctx context.Context, projectID string, page boundary.Page) ([]*timelineEvent, *postgres.PageKey, error) {
	starts, err := s.sessions.ReadRecentPreviewStarts(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	var events []*timelineEvent
	for _, record := range starts {
		kind := eventPreviewStarted
		if record.Applied {
			kind = eventPreviewConfigurationApplied
		}
		event := previewEvent(kind, record, record.AdmittedAt)
		event.Outcome = boundary.Enum[pb.StartOutcome](record.Outcome.String())
		event.Failure = boundary.FailureSnapshot(record.Failure())
		event.PhaseTimings = boundary.PhaseTimingSnapshots(record.Timings)
		events = append(events, &timelineEvent{id: record.CommandID, event: event})
	}
	slices.SortFunc(events, func(a, b *timelineEvent) int { return -comparePosition(a.key(), b.key()) })
	if page.After != nil {
		events = slices.DeleteFunc(events, func(e *timelineEvent) bool { return comparePosition(e.key(), *page.After) >= 0 })
	}
	events, next := postgres.Page(events, page.Size, (*timelineEvent).key)
	return events, next, nil
}

func comparePosition(a, b postgres.PageKey) int {
	if byTime := a.CreatedAt.Compare(b.CreatedAt); byTime != 0 {
		return byTime
	}
	return strings.Compare(a.ID, b.ID)
}

func previewEvent(kind environmentEventKind, record preview.StartRecord, at time.Time) *pb.EnvironmentEvent {
	return &pb.EnvironmentEvent{Kind: boundary.Enum[pb.EnvironmentEvent_Kind](kind), OccurredAt: timestamppb.New(at), ActorAccountId: record.ActorAccountID, Commit: record.Commit,
		ConfigurationRevision: record.ConfigurationRevision, Version: versionSnapshot(record.Version)}
}

// liveEvents derives the events of the Live history from the Release and
// Deployment read model.
func (s *projectServer) liveEvents(ctx context.Context, projectID string, page boundary.Page) ([]*timelineEvent, *postgres.PageKey, error) {
	listed, next, err := deployments.ListLiveEvents(ctx, s.deployments.Rows, projectID, page.After, page.Size)
	if err != nil {
		return nil, nil, err
	}
	events := make([]*timelineEvent, 0, len(listed))
	for _, e := range listed {
		kind := eventDeployedExistingVersion
		switch {
		case e.Cancelled:
			kind = eventPublishCancelled
		case e.Published:
			kind = eventPublished
		}
		events = append(events, &timelineEvent{id: e.ID, event: &pb.EnvironmentEvent{Kind: boundary.Enum[pb.EnvironmentEvent_Kind](kind), OccurredAt: timestamppb.New(e.OccurredAt), ActorAccountId: e.ActorAccountID,
			Commit: e.SourceCommit, Outcome: boundary.Enum[pb.StartOutcome](e.Outcome.String()), Failure: boundary.FailureSnapshot(e.Failure), PhaseTimings: boundary.PhaseTimingSnapshots(e.Timings),
			ReleaseId: e.ReleaseID, DeploymentId: e.DeploymentID, BuildRevisionId: e.BuildRevisionID, ConfigurationRevision: e.ConfigurationRevision,
			Version: versionSnapshot(e.Version), RolledBack: e.RolledBack, PreviewSkipReason: e.PreviewSkipReason}})
	}
	return events, next, nil
}

// nameActors sets the display name of each actor: the email of a member of
// the Project's TeamSpace, "a former member" for an account that left it.
func (s *projectServer) nameActors(ctx context.Context, teamSpaceID string, events []*timelineEvent) error {
	var ids []string
	for _, e := range events {
		if id := e.event.GetActorAccountId(); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	accounts, err := s.members.MemberAccounts(ctx, teamSpaceID, ids)
	if err != nil {
		return err
	}
	for _, e := range events {
		if id := e.event.GetActorAccountId(); id != "" {
			e.event.ActorDisplay = formerMember
			if account := accounts[id]; account.TeamSpaceMember {
				e.event.ActorDisplay = account.Email
			}
		}
	}
	return nil
}

// RecentTriggerEvents is the Slack event relay as the Project API reads it:
// the metadata of the events it recently routed to a scope.
type RecentTriggerEvents interface {
	RecentEvents(projectID, scope string) slackrelay.RecentEventsView
}

// ListRecentTriggerEvents returns the relay's recent events of the scope,
// newest first. Preview needs the builder capability, who work in it; Live
// the operator capability, who see Live. The relay's memory is the only
// source, so the list is empty after a restart and carries the time the
// process began recording.
func (s *projectServer) ListRecentTriggerEvents(ctx context.Context, in *pb.ListRecentTriggerEventsRequest) (*pb.ListRecentTriggerEventsResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	scope := infra.ScopeOf(in.GetScope())
	var capability projects.Capability
	switch scope.Kind {
	case infra.ScopePreview:
		capability = projects.CapabilityBuild
	case infra.ScopeLive:
		capability = projects.CapabilityOperateLive
	default:
		return nil, status.Error(codes.InvalidArgument, "scope must be PREVIEW or LIVE")
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), capability); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	view := s.recentEvents.RecentEvents(in.GetProjectId(), scope.Kind.String())
	out := &pb.ListRecentTriggerEventsResponse{BufferStartedAt: timestamppb.New(view.BufferStartedAt), BufferInstanceId: view.BufferInstanceID}
	for _, event := range view.Events {
		out.Events = append(out.Events, &pb.RecentTriggerEvent{
			ReceivedAt: timestamppb.New(event.ReceivedAt), EventId: event.EventID, EventType: event.EventType, EventSubtype: event.EventSubtype,
			TeamId: event.TeamID, ChannelId: event.ChannelID, ConnectionName: event.ConnectionName, Fate: boundary.Enum[pb.RecentTriggerEvent_Fate](event.Fate),
			HttpStatus: uint32(event.HTTPStatus), DurationMs: uint64(event.Duration.Milliseconds()),
		})
	}
	return out, nil
}
