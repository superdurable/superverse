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
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/agents/platformissue"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"google.golang.org/grpc/status"
)

// The platform-issue scenario starts a probe command in the background, whose
// end after the first turn opens an automatic turn. That turn corrupts the
// probe's record in the Sandbox's command ledger with a command and reads the
// probe twice with read_task_output: alone, and in a wave beside the readable
// dex-app.yaml. The ledger answers each read with a helper error, which the
// Sandbox tools treat as transient, so each read is repeated until its retries
// are exhausted and is recorded as the platform failure tool_unavailable: by
// RecordToolFailure for the serial read, and by the branch's
// RecoverParallelToolExecution and the join for the wave. The mock model goes
// on only when each result holds that code with its owner and effect, so the
// replies prove what the model read, and the automatic turn ends with the
// final text. The record is corrupted after the Session's watch of the probe
// recorded its end, so no other reader meets it.
//
// Each report starts the PlatformIssueFlow of its signature,
// platform-issue-test-<sig16>, so the reports of one signature address one
// FlowID. The deployed server names no issue repository (the local profiles
// leave github.issues empty), so that Flow only logs the incident. The test
// reads the Session's engine history with the Dex execution reader, as dexcli
// flow history --no-hydrate does, and requires that each Step that recorded a
// platform failure moved to CheckSteered and to ReportPlatformIssue in one
// decision, with the incident of the failed call, and that each
// ReportPlatformIssue ended as a dead end. It then requires one
// PlatformIssueFlow per signature whose latest run holds an incident of this
// Session, ran EnsurePlatformIssue and RecordPlatformIssue and recorded the
// outcome logged. The turn went on beside the reports, which it never waited
// for. That concurrent reports of a signature join one run is
// TestPlatformIssueJoinDexIntegration (internal/dexai/agents/platformissue).
func TestSessionMockPlatformIssueDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "platform-issue", 1).Content)
	session.waitForSnapshot(ctx, "the automatic turn that reads the corrupt ledger record", func(agent agentloop.AgentSnapshot) bool {
		return len(replies(agent.History)) >= 6 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	_, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 6 {
		t.Fatalf("expected the 6 mock replies, got %d; a stopped scenario answers with one text", len(assistant))
	}
	for _, reply := range assistant {
		requireMock(t, reply, mockInputTokens)
	}
	if final := assistant[5].Message; len(final.ToolCalls) != 0 || !strings.Contains(final.Content, "reported beside the turn") {
		t.Fatalf("the last reply is not the mock final text: %q", final.Content)
	}
	if notifications := ofRole(messages, conversation.MessageRoleNotification); len(notifications) != 1 ||
		notifications[0].Sequence < assistant[1].Sequence || notifications[0].Sequence > assistant[2].Sequence {
		t.Fatalf("expected the probe's one notification between the first turn and the automatic turn, got %+v", notifications)
	}
	failed := map[conversation.CallID]bool{}
	wantTools := []conversation.ToolName{conversation.ToolBash, conversation.ToolBash, conversation.ToolReadTaskOutput, conversation.ToolReadTaskOutput, conversation.ToolRead}
	wantCodes := []string{"", "", toolfailure.ToolUnavailable.String(), toolfailure.ToolUnavailable.String(), ""}
	tools := ofRole(messages, conversation.MessageRoleTool)
	if len(tools) != len(wantTools) {
		t.Fatalf("expected %d tool results, got %d", len(wantTools), len(tools))
	}
	for index, result := range tools {
		execution := result.Message.ToolExecution
		if *result.Message.ToolName != wantTools[index] || execution == nil || execution.ResultCode != wantCodes[index] || result.Message.ToolCallID == nil {
			t.Fatalf("tool result %d is %s with %+v; want %s with the code %q", index, *result.Message.ToolName, execution, wantTools[index], wantCodes[index])
		}
		if wantCodes[index] != "" {
			failed[*result.Message.ToolCallID] = true
		}
	}
	reports := session.platformIssueReports(ctx)
	if len(reports) != 2 {
		t.Fatalf("expected the reports of the serial read and of the wave, got %d: %+v", len(reports), reports)
	}
	for index, from := range []string{"RecordToolFailure", "AwaitParallelToolResults"} {
		report := reports[index]
		requireBesideTurn(t, report, from)
		incident := report.incident
		if !failed[incident.CallID] || incident.Tool != conversation.ToolReadTaskOutput || incident.Code != toolfailure.ToolUnavailable.String() ||
			incident.Effect != toolfailure.EffectNone.String() || incident.ErrorType == "" {
			t.Fatalf("the report from %s does not name the failed read: %+v", from, incident)
		}
		delete(failed, incident.CallID)
	}
	session.requireLoggedIssues(ctx, reports)
	t.Logf("FlowID=%s reports: %+v", session.id, reports)
}

// platformIssueReport is one report of a platform failure in a Session's engine
// history: the Step that recorded the failure and the incident it gave the
// side branch, whether that decision also continued the turn, and whether the
// side branch completed as a dead end once it started the report.
type platformIssueReport struct {
	from       string
	incident   agentloop.PlatformIncident
	besideTurn bool
	completed  bool
	ended      bool
}

// platformIssueReports reads the Session's whole engine history and returns
// its platform failure reports in history order. Every report must have
// completed: a turn that waited for the user has recorded its failures, but a
// report may finish after the turn did, so the read repeats until each has.
func (s *mockSession) platformIssueReports(ctx context.Context) []platformIssueReport {
	s.t.Helper()
	for {
		reports := s.readPlatformIssueReports(ctx)
		pending := false
		for _, report := range reports {
			pending = pending || !report.completed
		}
		if !pending {
			return reports
		}
		select {
		case <-ctx.Done():
			s.t.Fatalf("a platform issue report of %s never completed: %+v", s.id, reports)
		case <-time.After(2 * time.Second):
		}
	}
}

// readPlatformIssueReports reads the engine history of the Session's current
// run (runHistory; engine history is the one diagnostic read that uses the
// RunID) and returns its platform failure reports. A report whose start
// exhausted its retries fails the test at once.
func (s *mockSession) readPlatformIssueReports(ctx context.Context) []platformIssueReport {
	s.t.Helper()
	flowID := string(s.id)
	var reports []platformIssueReport
	recorders := map[string][]int{} // the recording Step execution: its reports
	for _, event := range runHistory(s.t, ctx, s.reader, flowID, currentRun(s.t, ctx, s.reader, flowID)) {
		completed := event.GetStepExecuteCompleted()
		if completed == nil {
			continue
		}
		step := completed.GetContext()
		decision := completed.GetOutput().GetStepDecision()
		switch step.GetStepType() {
		case "AbandonPlatformIssue":
			s.t.Fatalf("a report of %s could not start its PlatformIssueFlow and was abandoned", s.id)
		case "ReportPlatformIssue":
			// The reports of one recording Step differ only in their incidents,
			// which a completion does not name, so each completes the first open one.
			for _, index := range recorders[step.GetFromStepExecutionId()] {
				if !reports[index].completed {
					reports[index].completed = true
					reports[index].ended = len(decision.GetNextSteps()) == 0 &&
						decision.GetCloseDecision().GetCloseDecisionType() == dexpb.CloseDecisionType_CLOSE_DECISION_TYPE_DEAD_END
					break
				}
			}
			continue
		}
		movements := decision.GetNextSteps()
		besideTurn := false
		for _, movement := range movements {
			besideTurn = besideTurn || movement.GetStepType() == "CheckSteered"
		}
		for _, movement := range movements {
			if movement.GetStepType() == "ReportPlatformIssue" {
				var incident agentloop.PlatformIncident
				decodeHistoryJSON(ctx, s.t, s.reader, flowID, movement.GetStepInput(), &incident)
				recorders[step.GetStepExecutionId()] = append(recorders[step.GetStepExecutionId()], len(reports))
				reports = append(reports, platformIssueReport{from: step.GetStepType(), incident: incident, besideTurn: besideTurn})
			}
		}
	}
	return reports
}

// requireLoggedIssues requires one PlatformIssueFlow per signature of the
// reports, platform-issue-test-<sig16>, whose latest run holds an incident of
// this Session with the call of one of the signature's reports, ran
// EnsurePlatformIssue and RecordPlatformIssue, and recorded the outcome logged,
// as a server without an issue repository does.
func (s *mockSession) requireLoggedIssues(ctx context.Context, reports []platformIssueReport) {
	s.t.Helper()
	calls := map[string][]conversation.CallID{} // signature: the calls that reported it
	for _, report := range reports {
		calls[report.incident.Signature()] = append(calls[report.incident.Signature()], report.incident.CallID)
	}
	for signature, reported := range calls {
		flowID := platformissue.FlowIDForTest(deploymentnames.EnvironmentTest, signature)
		var state platformissue.SnapshotForTest
		s.await(ctx, "the outcome of "+flowID, func() bool {
			var err error
			state, err = s.issues.GetForTest(ctx, signature)
			return err == nil && state.Outcome != nil
		})
		incident := state.Incident
		if incident.SignatureForTest() != signature || incident.Environment != deploymentnames.EnvironmentTest || incident.SessionFlowID != string(s.id) ||
			!slices.Contains(reported, conversation.CallID(incident.CallID)) {
			s.t.Fatalf("%s does not hold an incident of the reports %v of %s: %+v", flowID, reported, s.id, incident)
		}
		if state.Outcome.Status != platformissue.OutcomeLoggedForTest || state.Outcome.IssueNumber != 0 {
			s.t.Fatalf("%s did not end after only logging; is an issue repository configured? %+v", flowID, *state.Outcome)
		}
		events := runHistory(s.t, ctx, s.reader, flowID, currentRun(s.t, ctx, s.reader, flowID))
		if steps := completedStepsAfter(events, 0); !slices.Equal(steps, []string{"EnsurePlatformIssue", "RecordPlatformIssue"}) {
			s.t.Fatalf("the run of %s completed %v", flowID, steps)
		}
		var record platformissue.OutcomeRecordForTest
		for _, event := range events {
			if completed := event.GetStepExecuteCompleted(); completed != nil && completed.GetContext().GetStepType() == "RecordPlatformIssue" {
				decodeHistoryJSON(ctx, s.t, s.reader, flowID, recordedEvent(s.t, completed.GetOutput().GetRecordEvents()), &record)
			}
		}
		if record.Signature != signature || record.Outcome != platformissue.OutcomeLoggedForTest.String() {
			s.t.Fatalf("%s recorded %+v", flowID, record)
		}
		s.t.Logf("%s: the reports %v of one signature address it; its run logged %s of %s", flowID, reported, incident.CallID, incident.SessionFlowID)
	}
}

// grpcFailure is the gRPC code and message of a failed engine read, so that a
// failure says why; the engine's read errors name the request, not payloads.
func grpcFailure(err error) string {
	failure := status.Convert(err)
	return failure.Code().String() + ": " + failure.Message()
}

// recordedEvent is the value of the platform issue event a RecordPlatformIssue
// recorded.
func recordedEvent(t *testing.T, events []*dexpb.KV) *dexpb.Value {
	t.Helper()
	for _, event := range events {
		if event.GetKey() == platformissue.OutcomeEventForTest {
			return event.GetValue()
		}
	}
	t.Fatalf("a RecordPlatformIssue completed without its %s event", platformissue.OutcomeEventForTest)
	return nil
}

// decodeHistoryJSON decodes a JSON object value of the engine history of
// flowID into target. The history holds a value inline or keeps it behind a
// blob ID; GetHistoryEvents does not hydrate, so a blob is loaded through
// LoadBlobs first, as Studio's Flow API does (flowapi hydrateBlob).
func decodeHistoryJSON(ctx context.Context, t *testing.T, reader dexpb.FlowServiceClient, flowID string, value *dexpb.Value, target any) {
	t.Helper()
	var blobID string
	switch kind := value.GetKind().(type) {
	case *dexpb.Value_InternalBlobIdForObjValue:
		blobID = kind.InternalBlobIdForObjValue
	case *dexpb.Value_InternalBlobIdForStringValue:
		blobID = kind.InternalBlobIdForStringValue
	}
	if blobID != "" {
		loaded, err := reader.LoadBlobs(ctx, &dexpb.LoadBlobsRequest{Entries: []*dexpb.LoadBlobRequestEntry{{FlowId: flowID, BlobValue: value}}})
		if err != nil {
			t.Fatalf("a history value of %s did not load (%s)", flowID, grpcFailure(err))
		}
		if value = loaded.GetValues()[blobID]; value == nil {
			t.Fatalf("LoadBlobs answered no value for a history blob of %s", flowID)
		}
	}
	object := value.GetObjValue()
	if object == nil || object.GetEncoding() != "json" {
		t.Fatalf("a history value of %s is no JSON object: %T", flowID, value.GetKind())
	}
	if err := json.Unmarshal(object.GetPayload(), target); err != nil {
		t.Fatal(err)
	}
}

// requireBesideTurn requires a report that the Step from started beside the
// turn, with an incident that classifies a platform failure, and that ended as
// a dead end once it started its PlatformIssueFlow.
func requireBesideTurn(t *testing.T, report platformIssueReport, from string) {
	t.Helper()
	if report.from != from || !report.besideTurn {
		t.Fatalf("the report came from %s beside the turn=%t, want %s beside the turn", report.from, report.besideTurn, from)
	}
	if !report.ended {
		t.Fatalf("the report from %s did not end as a dead end: %+v", from, report)
	}
	if report.incident.Owner != toolfailure.OwnerPlatform.String() || report.incident.At.IsZero() {
		t.Fatalf("the report from %s does not classify a platform failure: %+v", from, report.incident)
	}
}
