// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	evolveflow "github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// evolveServer serves EvolveService. EvolveFlow owns the goal measurement of
// each Project; the read model authorizes the caller first.
type evolveServer struct {
	pb.UnimplementedEvolveServiceServer
	*boundary.Boundary
	evolve   *evolveflow.EvolveFlowClient
	projects *projects.ProjectFlowClient
	settings evolveflow.Settings
}

// Register serves EvolveService on server.
func Register(server *grpc.Server, b *boundary.Boundary, client *evolveflow.EvolveFlowClient, projectClient *projects.ProjectFlowClient, settings evolveflow.Settings) {
	pb.RegisterEvolveServiceServer(server, &evolveServer{Boundary: b, evolve: client, projects: projectClient, settings: settings})
}

// errNotMeasured answers a Project without goal measurement, which Studio shows as its empty state.
var errNotMeasured = status.Error(codes.NotFound, "the Project has had no Live deployment, so nothing measures its goals yet")

// GetEvolve reads a Project's goal measurement for a caller who may operate its Live.
func (s *evolveServer) GetEvolve(ctx context.Context, in *pb.GetEvolveRequest) (*pb.GetEvolveResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	projection, err := s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityOperateLive)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	snapshot, err := s.evolve.Get(ctx, in.GetProjectId())
	if errors.Is(err, evolveflow.ErrNotStarted) {
		return nil, errNotMeasured
	}
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.GetEvolveResponse{Evolve: evolveSnapshot(snapshot, s.settings.ReviewsOn(projection.Project.TeamSpaceID), time.Now())}, nil
}

// StartEvolveReview starts a review now for a caller who may build the
// Project; a refusal is an answer, not an error.
func (s *evolveServer) StartEvolveReview(ctx context.Context, in *pb.StartEvolveReviewRequest) (*pb.StartEvolveReviewResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityBuild); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	admission, err := s.evolve.StartReview(ctx, in.GetProjectId(), evolveflow.ReviewRequest{RequestID: in.GetRequestId(), Actor: account.AccountID})
	if errors.Is(err, evolveflow.ErrNotStarted) {
		return nil, errNotMeasured
	}
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.StartEvolveReviewResponse{ReviewId: admission.ReviewID, Refusal: boundary.Enum[pb.EvolveReviewRefusal](admission.Refusal)}, nil
}

// DecideEvolveProposal records a decision on a review's offered proposal for
// a caller who may build the Project; a stale revision is an answer, not an error.
func (s *evolveServer) DecideEvolveProposal(ctx context.Context, in *pb.DecideEvolveProposalRequest) (*pb.DecideEvolveProposalResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	decision, err := boundary.DomainValue(in.GetDecision())
	if err != nil {
		return nil, err
	}
	reason := in.GetReason()
	for _, invalid := range []error{
		boundary.InvalidIf(evolveflow.ValidateReviewID(in.GetReviewId()) != nil, "review_id must name a review r-<n>"),
		boundary.InvalidIf(in.GetRevision() == 0, "revision must be the proposal revision the page showed"),
		boundary.InvalidIf(!utf8.ValidString(reason) || utf8.RuneCountInString(reason) > evolveflow.MaximumReasonRunes, "reason must be at most 500 characters"),
	} {
		if invalid != nil {
			return nil, invalid
		}
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityBuild); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	answer, err := s.evolve.DecideProposal(ctx, in.GetProjectId(), evolveflow.DecisionRequest{
		ReviewID: in.GetReviewId(), Revision: in.GetRevision(), Decision: evolveflow.ProposalDecision(strings.ToLower(decision)), Reason: reason, Actor: account.AccountID,
	})
	if errors.Is(err, evolveflow.ErrNotStarted) {
		return nil, errNotMeasured
	}
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.DecideEvolveProposalResponse{Refusal: boundary.Enum[pb.EvolveProposalRefusal](answer.Refusal)}
	if answer.Proposal != nil {
		out.Proposal = proposalSnapshot(*answer.Proposal)
	}
	return out, nil
}

// GetEvolveProposal reads the proposal document of a review for a caller
// who may operate the Project's Live.
func (s *evolveServer) GetEvolveProposal(ctx context.Context, in *pb.GetEvolveProposalRequest) (*pb.GetEvolveProposalResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.InvalidIf(evolveflow.ValidateReviewID(in.GetReviewId()) != nil, "review_id must name a review r-<n>"); err != nil {
		return nil, err
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityOperateLive); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	document, err := s.evolve.ReadProposal(ctx, in.GetProjectId(), in.GetReviewId())
	switch {
	case errors.Is(err, evolveflow.ErrNotStarted):
		return nil, errNotMeasured
	case errors.Is(err, evolveflow.ErrNoProposal):
		return nil, errNoProposal
	case err != nil:
		return nil, boundary.RPCStatus(err)
	}
	return &pb.GetEvolveProposalResponse{Markdown: string(document)}, nil
}

// errNoProposal answers a review without a kept proposal.
var errNoProposal = status.Error(codes.NotFound, "the review has no proposal")

// evolveSnapshot is the public view of a goal measurement.
func evolveSnapshot(s evolveflow.Snapshot, agentReviews bool, now time.Time) *pb.Evolve {
	out := &pb.Evolve{
		ProjectId: s.ProjectID, ActiveReviewId: s.Active, AgentReviews: agentReviews, NextManualReviewAt: timestamp(s.NextManualReviewAt(now)),
		Live:  &pb.EvolveLive{Revision: s.Live.Revision, ReleaseId: s.Live.ReleaseID, BuildCommit: s.Live.BuildCommit, CommittedAt: timestamp(s.Live.CommittedAt)},
		Goals: goalsSnapshot(s.Goals),
		Schedule: &pb.EvolveSchedule{
			Period: boundary.Enum[pb.EvolveReviewPeriod](s.Schedule.Period), Active: s.Schedule.Active, NextAt: timestamp(s.Schedule.NextAt),
		},
	}
	for _, review := range s.Reviews {
		out.Reviews = append(out.Reviews, reviewSnapshot(review))
	}
	for _, proposal := range s.Proposals {
		out.Proposals = append(out.Proposals, proposalSnapshot(proposal))
	}
	return out
}

func proposalSnapshot(p evolveflow.ProposalRecord) *pb.EvolveProposal {
	out := &pb.EvolveProposal{
		ReviewId: p.ReviewID, Kind: boundary.Enum[pb.EvolveProposalKind](p.Kind), Undoes: p.Undoes, Title: p.Title, Summary: p.Summary,
		AgentId: p.AgentID, BacktestJson: backtestJSON(p.Backtest), Status: boundary.Enum[pb.EvolveProposalStatus](p.Status), StatusAt: timestamp(p.StatusAt),
		Revision: p.Revision, OfferedAt: timestamp(p.OfferedAt), NotifyFailed: p.NotifyFailed, AppliedCommit: p.AppliedCommit,
		AppliedAt: timestamp(p.AppliedAt), LiveRevision: p.LiveRevision, LiveAt: timestamp(p.LiveAt), PreviousLiveReleaseId: p.PreviousLiveReleaseID,
	}
	if d := p.Decision; d != nil {
		out.Decision = &pb.EvolveProposalDecisionRecord{
			Decision: boundary.Enum[pb.EvolveProposalDecision](d.Decision), ActorAccountId: d.Actor, DecidedAt: timestamp(d.At), Reason: d.Reason,
		}
	}
	return out
}

// backtestJSON is the stored summary of a proposal's backtest as JSON, with
// the keys of backtest.json; empty for none.
func backtestJSON(summary *goals.BacktestSummary) string {
	if summary == nil {
		return ""
	}
	return string(comm.Must(json.Marshal(summary)))
}

func goalsSnapshot(g evolveflow.Goals) *pb.EvolveGoals {
	spec := g.Parsed()
	out := &pb.EvolveGoals{Commit: g.Commit, Status: boundary.Enum[pb.EvolveGoalsStatus](g.Status), Errors: g.Errors, ReadAt: timestamp(g.ReadAt), FlowTypes: spec.Flows}
	for _, goal := range spec.Goals {
		out.Goals = append(out.Goals, goalSnapshot(goal))
	}
	return out
}

func goalSnapshot(goal goals.Goal) *pb.EvolveGoal {
	out := &pb.EvolveGoal{
		Id: goal.ID, Kind: boundary.Enum[pb.EvolveGoalKind](goal.Kind), Statement: goal.Statement, Metric: goal.Metric,
		Direction: boundary.Enum[pb.EvolveGoalDirection](goal.Direction), Target: goal.Target,
	}
	if goal.Guardrail != nil {
		out.GuardrailMax, out.GuardrailMin = goal.Guardrail.Max, goal.Guardrail.Min
	}
	return out
}

func reviewSnapshot(r evolveflow.ReviewRecord) *pb.EvolveReview {
	out := &pb.EvolveReview{
		ReviewId: r.ID, Trigger: boundary.Enum[pb.EvolveReviewTrigger](r.Trigger), ActorAccountId: r.Actor,
		RequestedAt: timestamp(r.RequestedAt), StartedAt: timestamp(r.StartedAt), FinishedAt: timestamp(r.FinishedAt),
		WindowFrom: timestamp(r.Window.From), WindowTo: timestamp(r.Window.To), GoalsCommit: r.GoalsCommit,
		Status: boundary.Enum[pb.EvolveReviewStatus](r.Status), Reason: boundary.Enum[pb.EvolveReviewReason](r.Reason), Detail: r.Detail,
		Export: &pb.EvolveExport{
			Parts: count(r.Export.Parts), Runs: count(r.Export.Runs), Bytes: r.Export.Bytes, Truncated: r.Export.Truncated, Deleted: r.Export.Deleted,
		},
	}
	if r.Metrics != nil {
		out.Runs = count(r.Metrics.Runs)
		for _, goal := range r.Metrics.Goals {
			out.Values = append(out.Values, &pb.EvolveGoalValue{GoalId: goal.ID, Value: goal.Value, Runs: count(goal.Runs), FewRuns: goal.FewRuns, Holds: goal.Holds})
		}
	}
	return out
}

// count is a bounded non-negative count of the Flow's own records.
func count(n int) uint32 { return uint32(max(n, 0)) } // #nosec G115 -- the Flow bounds every count far below 2^32.

// timestamp is the protobuf time of at, absent for the zero time.
func timestamp(at time.Time) *timestamppb.Timestamp {
	if at.IsZero() {
		return nil
	}
	return timestamppb.New(at)
}
