// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// Test-only scheduling counter: it owns no Stop fact. All marker/state changes
// below run the actual production handlers with their actual locks and loads.
var stopMarkerSettled = dex.DefineAttribute[int]("TestStopHandlerSettled")

// StopMarkerOrderingFlow is registered only on this test's own real Worker.
// It checks Dex commits, identity and persistence, not task/provider effects.
type StopMarkerOrderingFlow struct{ dex.FlowDefaults }

func (*StopMarkerOrderingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	schema := (&SessionAgentFlow{}).GetPersistenceSchema()
	schema.Attributes = append(schema.Attributes, stopMarkerSettled)
	return schema
}
func (*StopMarkerOrderingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStep(SettleStopMarkerTest{}), dex.DefineStep(ConsumeStopMarkerTest{}), dex.DefineStep(EndStopMarkerTest{}), dex.DefineStep(RouteStoppedToolTest{}), dex.DefineStep(ConsumeStaleRemoteTest{}), dex.DefineStep(ConsumeStaleIdleTest{})}
}
func (flow *StopMarkerOrderingFlow) GetRPCs() []dex.RPCDef {
	options := &dex.RPCOptions{Timeout: time.Minute, IsTransactional: true, LockAttributes: finishInterruptStepOptions.ExecuteLockAttributes, LoadAttributeMaps: taskMessageMaps}
	return []dex.RPCDef{dex.DefineRPC(flow.InstallStop, options), dex.DefineRPC(flow.InstallRemoteReport, options), dex.DefineRPC(flow.CommitIdleRemoteStop, options), dex.DefineRPC(flow.ScheduleStaleRemote, options), dex.DefineRPC(flow.ScheduleSettlement, options), dex.DefineRPC(flow.ScheduleStoppedRoute, options), dex.DefineRPC(flow.WakeWithInput, options), dex.DefineRPC(flow.WakeWithNotification, options), dex.DefineRPC(flow.ReadCommittedStop, &dex.RPCOptions{Timeout: time.Minute, LoadAttributeMaps: taskMessageMaps}), dex.DefineRPC(flow.CompleteStopTest, options)}
}

// Like sessionStartAttributes, the test fixture is installed with StartFlow,
// so RPCs can read it before any Step executes.
func stopMarkerTestStartAttributes() []dex.InitialAttributeDef {
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, agentStateAttribute, agentloop.AgentState{Turn: 7, Status: agentloop.AgentStatusWaitingForMessage, CurrentFirstSequence: 1, FirstRetainedSequence: 1, PendingToolCalls: []conversation.ToolCall{}})
	dexflow.Add(attributes, sessionStatusAttribute, agentloop.SessionStatusActive)
	dexflow.Add(attributes, headCommitAttribute, "stop-ordering-test")
	dexflow.Add(attributes, messageWindowLockAttribute, false)
	dexflow.Add(attributes, waitingInputRoundAttribute, agentloop.WaitingInputRound(0))
	dexflow.Add(attributes, stopMarkerSettled, 0)
	return attributes.Values()
}

type stopFixture struct {
	Interrupt     agentloop.AgentInterrupt `json:"interrupt"`
	StateTurn     int64                    `json:"state_turn"`
	Status        agentloop.AgentStatus    `json:"status"`
	Starting      bool                     `json:"starting"`
	DropInterrupt bool                     `json:"drop_interrupt"`
	PendingCalls  []conversation.ToolCall  `json:"pending_calls,omitempty"`
	Executor      *remoteexec.Executor     `json:"executor,omitempty"`
}

func (*StopMarkerOrderingFlow) InstallStop(ctx dex.Context, fixture stopFixture) (*dex.RPCResult[dex.None], error) {
	state := comm.Must(agentStateAttribute.Get(ctx))
	state.Turn, state.Status = fixture.StateTurn, fixture.Status
	if len(fixture.PendingCalls) > 0 {
		state.PendingToolCalls, state.PendingToolIndex = fixture.PendingCalls, 0
	}
	if fixture.Executor != nil {
		state.PendingToolsAdmission = fixture.Executor.Admitted()
		comm.MustDo(executorAttribute.Set(ctx, *fixture.Executor))
	}
	comm.MustDo(agentStateAttribute.Set(ctx, state))
	if fixture.DropInterrupt {
		comm.MustDo(agentInterruptAttribute.Delete(ctx))
	} else {
		marked := fixture.Interrupt.MarkerWritten
		fixture.Interrupt.MarkerWritten = false
		comm.MustDo(agentInterruptAttribute.Set(ctx, fixture.Interrupt))
		if marked {
			writeStopMarker(ctx, &fixture.Interrupt)
		}
	}
	if fixture.Starting {
		record := agenttasks.NewSubagentRecord("teststart", "test-start-call", agenttasks.SubagentCall{Kind: agenttasks.SubagentInvestigate, Description: "Test starting-subagent cleanup"}, "fixture", time.Now().UTC())
		comm.MustDo(subagentsAttribute.Set(ctx, record.AgentID, record))
	}
	return &dex.RPCResult[dex.None]{}, nil
}

// InstallRemoteReport commits the already admitted first report before Stop.
func (*StopMarkerOrderingFlow) InstallRemoteReport(ctx dex.Context, report remoteexec.RemoteReport) (*dex.RPCResult[dex.None], error) {
	executor := remoteexec.Executor{Mode: remoteexec.ModeRemote, Epoch: report.Epoch, Generation: report.Generation,
		Remote: &remoteexec.RemoteExecutor{ConnectionID: report.ConnectionID, Owner: report.Owner, Reported: report.Sequence}}
	comm.MustDo(executorAttribute.Set(ctx, executor))
	comm.MustDo(remoteInboxChannel.Publish(ctx, remoteexec.InboxItem{Report: &report}))
	return &dex.RPCResult[dex.None]{}, nil
}

// CommitIdleRemoteStop calls the production admission and marker consumers in
// separate Dex commits from the report. An idle Stop has no running Step for
// InterruptAgent to cancel, and AwaitUser would consume its marker before any
// later report, so the fixture commits that same consumption explicitly.
func (*StopMarkerOrderingFlow) CommitIdleRemoteStop(ctx dex.Context, actor conversation.Actor) (*dex.RPCResult[bool], error) {
	_, plan, err := interruptAgent(ctx, agentloop.InterruptAgentRequest{Turn: 7, Actor: &actor})
	if err != nil {
		return nil, err
	}
	if !plan.Admitted || plan.Joined {
		return &dex.RPCResult[bool]{Output: false}, nil
	}
	takeAgentInterrupt(ctx, true)
	return &dex.RPCResult[bool]{Output: true}, nil
}

type staleRemoteFixture struct {
	Boundary int  `json:"boundary"`
	Idle     bool `json:"idle"`
}

func (*StopMarkerOrderingFlow) ScheduleStaleRemote(_ dex.Context, input staleRemoteFixture) (*dex.RPCResult[dex.None], error) {
	if input.Idle {
		return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(ConsumeStaleIdleTest{}, input.Boundary)}}, nil
	}
	return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(ConsumeStaleRemoteTest{}, input.Boundary)}}, nil
}

type ConsumeStaleRemoteTest struct{ dex.StepDefaults }

func (ConsumeStaleRemoteTest) GetStepOptions() *dex.StepOptions { return remoteStepOptions() }
func (ConsumeStaleRemoteTest) WaitFor(ctx dex.Context, _ int) (*dex.Wait, error) {
	return (AwaitRemoteAgent{}).WaitFor(ctx, nil)
}
func (ConsumeStaleRemoteTest) Execute(ctx dex.Context, boundary int) (*dex.StepDecision, error) {
	flow := &SessionAgentFlow{automaticTurns: agentloop.AutomaticTurnBounds{Task: 5, Platform: 5}, sandboxHold: time.Minute}
	if _, err := (AwaitRemoteAgent{flow: flow}).Execute(ctx, nil); err != nil {
		return nil, err
	}
	comm.MustDo(stopMarkerSettled.Set(ctx, boundary))
	return dex.DeadEnd(), nil
}

type ConsumeStaleIdleTest struct{ dex.StepDefaults }

func (ConsumeStaleIdleTest) GetStepOptions() *dex.StepOptions { return remoteIdleStepOptions() }
func (ConsumeStaleIdleTest) WaitFor(ctx dex.Context, _ int) (*dex.Wait, error) {
	return (AwaitUser{}).WaitFor(ctx, nil)
}
func (ConsumeStaleIdleTest) Execute(ctx dex.Context, boundary int) (*dex.StepDecision, error) {
	flow := &SessionAgentFlow{automaticTurns: agentloop.AutomaticTurnBounds{Task: 5, Platform: 5}, sandboxHold: time.Minute}
	if _, err := (AwaitUser{flow: flow}).Execute(ctx, nil); err != nil {
		return nil, err
	}
	comm.MustDo(stopMarkerSettled.Set(ctx, boundary))
	return dex.DeadEnd(), nil
}

type settlementFixture struct {
	// Raw JSON is persisted as the Step input, including the old shape with no
	// requested_at. Decoding happens after the real Worker loads that Step.
	Interruption json.RawMessage `json:"interruption"`
	Boundary     int             `json:"boundary"`
}

func (*StopMarkerOrderingFlow) ScheduleSettlement(_ dex.Context, input settlementFixture) (*dex.RPCResult[dex.None], error) {
	return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(SettleStopMarkerTest{}, input)}}, nil
}

type SettleStopMarkerTest struct {
	dex.StepDefaultsNoWaitFor[settlementFixture]
}

func (SettleStopMarkerTest) GetStepOptions() *dex.StepOptions { return finishInterruptStepOptions }
func (SettleStopMarkerTest) Execute(ctx dex.Context, input settlementFixture) (*dex.StepDecision, error) {
	var interruption agentInterruption
	comm.MustDo(json.Unmarshal(input.Interruption, &interruption))
	if _, err := (FinishInterrupt{}).Execute(ctx, interruption); err != nil {
		return nil, err
	}
	comm.MustDo(stopMarkerSettled.Set(ctx, input.Boundary))
	// Hold the boundary for the next test operation instead of scheduling model
	// work. This changes only test scheduling, not the production commit logic.
	return dex.DeadEnd(), nil
}

type wakeFixture struct {
	Boundary     int                              `json:"boundary"`
	Message      conversation.PendingUserMessage  `json:"message"`
	Notification conversation.SessionNotification `json:"notification"`
}

func (*StopMarkerOrderingFlow) WakeWithInput(ctx dex.Context, input wakeFixture) (*dex.RPCResult[dex.None], error) {
	comm.MustDo(queuedUserMessagesChannel.Publish(ctx, input.Message))
	return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(ConsumeStopMarkerTest{}, input.Boundary)}}, nil
}
func (*StopMarkerOrderingFlow) WakeWithNotification(ctx dex.Context, input wakeFixture) (*dex.RPCResult[dex.None], error) {
	comm.MustDo(sessionNotificationsChannel.Publish(ctx, input.Notification))
	return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(ConsumeStopMarkerTest{}, input.Boundary)}}, nil
}

type ConsumeStopMarkerTest struct{ dex.StepDefaults }

func (ConsumeStopMarkerTest) GetStepOptions() *dex.StepOptions { return remoteIdleStepOptions() }
func (ConsumeStopMarkerTest) WaitFor(ctx dex.Context, _ int) (*dex.Wait, error) {
	return (AwaitUser{}).WaitFor(ctx, nil)
}
func (ConsumeStopMarkerTest) Execute(ctx dex.Context, boundary int) (*dex.StepDecision, error) {
	flow := &SessionAgentFlow{automaticTurns: agentloop.AutomaticTurnBounds{Task: 5, Platform: 5}, sandboxHold: time.Minute}
	if _, err := (AwaitUser{flow: flow}).Execute(ctx, nil); err != nil {
		return nil, err
	}
	comm.MustDo(stopMarkerSettled.Set(ctx, boundary))
	return dex.DeadEnd(), nil
}

type committedStop struct {
	State     agentloop.AgentState                 `json:"state"`
	Interrupt *agentloop.AgentInterrupt            `json:"interrupt,omitempty"`
	Entries   []conversation.Entry                 `json:"entries"`
	Boundary  int                                  `json:"boundary"`
	Subagents map[string]agenttasks.SubagentRecord `json:"subagents"`
	Executor  remoteexec.Executor                  `json:"executor"`
}

func (*StopMarkerOrderingFlow) ReadCommittedStop(ctx dex.Context, _ dex.None) (*dex.RPCResult[committedStop], error) {
	result := committedStop{State: comm.Must(agentStateAttribute.Get(ctx)), Interrupt: dexflow.OptionalValue(agentInterruptAttribute.Get(ctx)), Boundary: comm.Must(stopMarkerSettled.Get(ctx)), Subagents: loadedSubagents(ctx), Executor: currentExecutor(ctx)}
	for seq := result.State.CurrentFirstSequence; seq <= result.State.LastSequence; seq++ {
		result.Entries = append(result.Entries, comm.Must(currentMessagesAttribute.Get(ctx, agentloop.SequenceKey(seq))))
	}
	return &dex.RPCResult[committedStop]{Output: result}, nil
}
func (*StopMarkerOrderingFlow) CompleteStopTest(_ dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(EndStopMarkerTest{}, nil)}}, nil
}

type EndStopMarkerTest struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (EndStopMarkerTest) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

// Every driver read is real Dex. Convergence waits for the Step's committed
// test counter, not a fixed sleep or a fabricated handler result.
func waitCommittedStop(t *testing.T, ctx context.Context, sdk *dex.Client, flow *StopMarkerOrderingFlow, id string, boundary int) committedStop {
	t.Helper()
	for {
		var result committedStop
		if err := sdk.InvokeRPC(ctx, id, flow.ReadCommittedStop, nil, &result); err != nil {
			t.Fatalf("ReadCommittedStop %s at boundary %d: %v", id, boundary, err)
		}
		if result.Boundary == boundary {
			return result
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func stopTestClient(t *testing.T, ctx context.Context) (*dex.Client, func() *dex.Client, *StopMarkerOrderingFlow, string) {
	t.Helper()
	flow := &StopMarkerOrderingFlow{}
	host := os.Getenv("SUPERVERSE_TEST_WORKER_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	sdk, replace := dexflowtest.WorkerClientWithReplacementForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), host, flow)
	id := "stop-marker-ordering-" + flowid.RandomID(16)
	ready := false
	defer func() {
		if !ready {
			completeStopTest(t, ctx, sdk, flow, id)
		}
	}()
	if _, err := sdk.StartFlow(ctx, flow, id, nil, dex.StartFlowOptions{RequestID: &id, IDReusePolicy: dex.IDReuseDisallow, Attributes: stopMarkerTestStartAttributes()}); err != nil {
		t.Fatal(err)
	}
	waitCommittedStop(t, ctx, sdk, flow, id, 0)
	ready = true
	return sdk, replace, flow, id
}
func completeStopTest(t *testing.T, ctx context.Context, sdk *dex.Client, flow *StopMarkerOrderingFlow, id string) {
	t.Helper()
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	var none dex.None
	err := sdk.InvokeRPC(cleanup, id, flow.CompleteStopTest, nil, &none)
	if err == nil {
		_, err = sdk.WaitForFlow(cleanup, id, dex.WaitForFlowOptions{})
	}
	if err != nil {
		termination, cancelTermination := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancelTermination()
		var inactive *dex.FlowNotActiveOrNotFoundError
		if stop := sdk.StopFlow(termination, id, dex.StopOptions{Type: dex.TerminateFlow, Reason: "Stop ordering test cleanup"}); stop != nil && !errors.As(stop, &inactive) {
			t.Errorf("test execution cleanup: %v", stop)
		}
		t.Errorf("test execution did not complete: %v", err)
	}
}
func requireOneStop(t *testing.T, result committedStop, actor conversation.Actor) {
	t.Helper()
	count := 0
	for _, entry := range result.Entries {
		if entry.Meta.Marker == "Stopped by "+actor.Email {
			count++
			if entry.Meta.Actor == nil || *entry.Meta.Actor != actor {
				t.Fatal("marker changed first actor")
			}
		}
	}
	if count != 1 {
		t.Fatalf("first-actor markers = %d, want 1", count)
	}
}

func TestStopMarkerCommitOrdersDexIntegration(t *testing.T) {
	for _, order := range []string{"notification-first", "input-first", "finish-first-notification", "finish-first-input", "worker-recreation"} {
		t.Run(order, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			sdk, replace, flow, id := stopTestClient(t, ctx)
			defer func() { completeStopTest(t, ctx, sdk, flow, id) }()
			actor := conversation.Actor{AccountID: "builder-a", Email: "builder-a@example.com"}
			pending := agentloop.AgentInterrupt{Turn: 7, RequestedAt: time.Now().UTC(), Actor: &actor}
			var none dex.None
			if err := sdk.InvokeRPC(ctx, id, flow.InstallStop, stopFixture{Interrupt: pending, StateTurn: 7, Status: agentloop.AgentStatusWaitingForMessage}, &none); err != nil {
				t.Fatal(err)
			}
			encoded := comm.Must(json.Marshal(agentInterruption{Turn: 7, RequestedAt: pending.RequestedAt, Actor: &actor}))
			finishFirst := order != "notification-first" && order != "input-first"
			boundary := 1
			if finishFirst {
				if err := sdk.InvokeRPC(ctx, id, flow.ScheduleSettlement, settlementFixture{encoded, boundary}, &none); err != nil {
					t.Fatal(err)
				}
				result := waitCommittedStop(t, ctx, sdk, flow, id, boundary)
				requireOneStop(t, result, actor)
				if result.Interrupt == nil || !result.Interrupt.MarkerWritten || result.State.AutomaticTurnsPaused {
					t.Fatal("idle Finish must retain suppression and mark once without pausing itself")
				}
				if order == "worker-recreation" {
					sdk = replace()
					cold := waitCommittedStop(t, ctx, sdk, flow, id, boundary)
					if !reflect.DeepEqual(result, cold) {
						t.Fatal("fresh Worker changed committed marker/interrupt state")
					}
				}
				boundary++
			}
			input := order == "input-first" || order == "finish-first-input"
			wake := wakeFixture{Boundary: boundary, Message: conversation.PendingUserMessage{MessageID: "input-b", Value: conversation.UserMessage{Content: "Explicit new input", Actor: &conversation.Actor{AccountID: "builder-b", Email: "builder-b@example.com"}}}, Notification: conversation.SessionNotification{ID: "task/completed", Kind: conversation.NotificationKindTask, Source: conversation.NotificationSourceCommand, TaskID: "cmd-aaaaaaaaaaaa", TaskStatus: conversation.TaskEndExited, Summary: "Real-Dex fixture completion"}}
			var err error
			if input {
				err = sdk.InvokeRPC(ctx, id, flow.WakeWithInput, wake, &none)
			} else {
				err = sdk.InvokeRPC(ctx, id, flow.WakeWithNotification, wake, &none)
			}
			if err != nil {
				t.Fatal(err)
			}
			result := waitCommittedStop(t, ctx, sdk, flow, id, boundary)
			requireOneStop(t, result, actor)
			if result.Interrupt != nil {
				t.Fatal("AwaitUser did not consume pending Stop")
			}
			if input {
				if result.State.Turn != 8 || result.State.AutomaticTurnsPaused || len(result.Entries) != 2 || result.Entries[0].Meta.Marker == "" || result.Entries[1].Kind != conversation.EntryKindUserMessage {
					t.Fatal("explicit input must follow marker and resume without a pause")
				}
			} else {
				if result.State.Turn != 7 || !result.State.AutomaticTurnsPaused || len(result.Entries) != 3 || result.Entries[0].Meta.Marker == "" || result.Entries[1].Kind != conversation.EntryKindNotification {
					t.Fatal("notification must follow marker and pause without opening a turn")
				}
			}
			before := result
			boundary++
			if err := sdk.InvokeRPC(ctx, id, flow.ScheduleSettlement, settlementFixture{encoded, boundary}, &none); err != nil {
				t.Fatal(err)
			}
			result = waitCommittedStop(t, ctx, sdk, flow, id, boundary)
			before.Boundary = boundary
			if !reflect.DeepEqual(before, result) {
				t.Fatal("late settlement changed consumed Stop or resumed turn")
			}
		})
	}
}

func TestStopMarkerIdentityAndLegacyDexIntegration(t *testing.T) {
	for _, mode := range []string{"new-same-turn", "legacy-ending", "legacy-ending-task-only", "legacy-ending-written", "legacy-new-turn", "legacy-non-ending", "legacy-no-pending", "ending-already-written"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			sdk, _, flow, id := stopTestClient(t, ctx)
			defer completeStopTest(t, ctx, sdk, flow, id)
			first := conversation.Actor{AccountID: "first", Email: "first@example.com"}
			next := conversation.Actor{AccountID: "next", Email: "next@example.com"}
			pending := agentloop.AgentInterrupt{Turn: 7, RequestedAt: time.Now().UTC(), Actor: &next, EndsTurn: mode != "new-same-turn" && mode != "legacy-ending-task-only" && mode != "legacy-non-ending", MarkerWritten: mode == "legacy-ending-written" || mode == "ending-already-written"}
			turn, status := int64(7), agentloop.AgentStatusCallingModel
			if mode == "new-same-turn" || mode == "legacy-ending-task-only" || mode == "legacy-non-ending" {
				status = agentloop.AgentStatusWaitingForMessage
			}
			if mode == "legacy-new-turn" {
				turn = 8
			}
			fixture := stopFixture{Interrupt: pending, StateTurn: turn, Status: status, Starting: mode == "legacy-non-ending", DropInterrupt: mode == "legacy-no-pending"}
			var none dex.None
			if err := sdk.InvokeRPC(ctx, id, flow.InstallStop, fixture, &none); err != nil {
				t.Fatal(err)
			}
			raw := json.RawMessage(`{"turn":7,"ends_turn":true,"actor":{"account_id":"first","email":"first@example.com"}}`)
			if mode == "ending-already-written" {
				raw = comm.Must(json.Marshal(agentInterruption{Turn: 7, RequestedAt: pending.RequestedAt, EndsTurn: true, Actor: &first}))
			}
			if mode == "new-same-turn" {
				raw = comm.Must(json.Marshal(agentInterruption{Turn: 7, RequestedAt: pending.RequestedAt.Add(-time.Second), Actor: &first}))
			}
			if mode == "legacy-non-ending" {
				raw = json.RawMessage(`{"turn":7,"actor":{"account_id":"first","email":"first@example.com"}}`)
			}
			boundary := 0
			if mode == "new-same-turn" {
				// Settle and consume the earlier Stop through the actual handlers first.
				// The idle notification leaves Turn unchanged; install the distinct newer
				// pending fixture only after that commit, then replay the older settlement.
				older := pending
				older.RequestedAt, older.Actor = pending.RequestedAt.Add(-time.Second), &first
				if err := sdk.InvokeRPC(ctx, id, flow.InstallStop, stopFixture{Interrupt: older, StateTurn: 7, Status: agentloop.AgentStatusWaitingForMessage}, &none); err != nil {
					t.Fatal(err)
				}
				if err := sdk.InvokeRPC(ctx, id, flow.ScheduleSettlement, settlementFixture{raw, 1}, &none); err != nil {
					t.Fatal(err)
				}
				waitCommittedStop(t, ctx, sdk, flow, id, 1)
				wake := wakeFixture{Boundary: 2, Notification: conversation.SessionNotification{ID: "task/older-completed", Kind: conversation.NotificationKindTask, Source: conversation.NotificationSourceCommand, TaskID: "cmd-bbbbbbbbbbbb", TaskStatus: conversation.TaskEndExited, Summary: "Earlier fixture completion"}}
				if err := sdk.InvokeRPC(ctx, id, flow.WakeWithNotification, wake, &none); err != nil {
					t.Fatal(err)
				}
				consumed := waitCommittedStop(t, ctx, sdk, flow, id, 2)
				requireOneStop(t, consumed, first)
				if consumed.Interrupt != nil || consumed.State.Turn != 7 {
					t.Fatal("earlier idle Stop was not consumed in its original turn")
				}
				if err := sdk.InvokeRPC(ctx, id, flow.InstallStop, fixture, &none); err != nil {
					t.Fatal(err)
				}
				boundary = 2
			}
			before := waitCommittedStop(t, ctx, sdk, flow, id, boundary)
			boundary++
			if err := sdk.InvokeRPC(ctx, id, flow.ScheduleSettlement, settlementFixture{raw, boundary}, &none); err != nil {
				t.Fatal(err)
			}
			result := waitCommittedStop(t, ctx, sdk, flow, id, boundary)
			if mode == "legacy-ending" || mode == "ending-already-written" {
				requireOneStop(t, result, next)
				if result.Interrupt != nil || result.State.Status != agentloop.AgentStatusWaitingForMessage || !result.State.AutomaticTurnsPaused {
					t.Fatal("legacy ending input did not recover using durable pending actor")
				}
			} else {
				before.Boundary = boundary
				if mode == "legacy-non-ending" {
					record := result.Subagents["teststart"]
					if record.Status != agenttasks.SubagentStopped {
						t.Fatal("legacy task-only cleanup did not stop STARTING subagent")
					}
					before.Subagents = result.Subagents
				}
				if !reflect.DeepEqual(before, result) {
					t.Fatal("stale/legacy input mutated newer interrupt, marker or turn")
				}
			}
		})
	}
}

// ScheduleStoppedRoute places RouteTool after an already committed Stop, the
// exact admission-to-run window between CheckSteered and RouteTool.
func (*StopMarkerOrderingFlow) ScheduleStoppedRoute(_ dex.Context, boundary int) (*dex.RPCResult[dex.None], error) {
	return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(RouteStoppedToolTest{}, boundary)}}, nil
}

type RouteStoppedToolTest struct{ dex.StepDefaultsNoWaitFor[int] }

func (RouteStoppedToolTest) GetStepOptions() *dex.StepOptions { return routeToolStepOptions }
func (RouteStoppedToolTest) Execute(ctx dex.Context, boundary int) (*dex.StepDecision, error) {
	// A missing ContextSegment and dependency are intentional: a stopped route
	// must return before reading the catalog, running built-ins or launching work.
	if _, err := (RouteTool{flow: &SessionAgentFlow{}}).Execute(ctx, nil); err != nil {
		return nil, err
	}
	comm.MustDo(stopMarkerSettled.Set(ctx, boundary))
	return dex.DeadEnd(), nil
}

func TestRouteToolStopFenceDexIntegration(t *testing.T) {
	for _, mode := range []remoteexec.Mode{remoteexec.ModePlatform, remoteexec.ModeRemote} {
		for _, kind := range []string{"built-in", "parallel-wave"} {
			t.Run(string(mode)+"/"+kind, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				sdk, _, flow, id := stopTestClient(t, ctx)
				defer completeStopTest(t, ctx, sdk, flow, id)
				actor := conversation.Actor{AccountID: "stopper", Email: "stopper@example.com"}
				calls := []conversation.ToolCall{{ID: "fenced-todos", Name: conversation.ToolWriteTodos, Arguments: conversation.MustJSONObject(`{"todos":[]}`)}}
				if kind == "parallel-wave" {
					calls = []conversation.ToolCall{{ID: "fenced-read-a", Name: conversation.ToolRead, Arguments: conversation.MustJSONObject(`{"path":"a.txt"}`)},
						{ID: "fenced-read-b", Name: conversation.ToolRead, Arguments: conversation.MustJSONObject(`{"path":"b.txt"}`)}}
				}
				fixture := stopFixture{Interrupt: agentloop.AgentInterrupt{Turn: 7, RequestedAt: time.Now().UTC(), Actor: &actor}, StateTurn: 7, Status: agentloop.AgentStatusRoutingTool, PendingCalls: calls}
				if mode == remoteexec.ModeRemote {
					fixture.Executor = &remoteexec.Executor{Mode: remoteexec.ModeRemote, Epoch: 3, Generation: 5,
						Remote: &remoteexec.RemoteExecutor{ConnectionID: "route-stop", Owner: actor}}
				}
				var none dex.None
				if err := sdk.InvokeRPC(ctx, id, flow.InstallStop, fixture, &none); err != nil {
					t.Fatal(err)
				}
				if err := sdk.InvokeRPC(ctx, id, flow.ScheduleStoppedRoute, 1, &none); err != nil {
					t.Fatal(err)
				}
				result := waitCommittedStop(t, ctx, sdk, flow, id, 1)
				requireOneStop(t, result, actor)
				if result.Interrupt != nil || result.State.Running != nil || len(result.State.PendingToolCalls) != 0 || result.State.Status != agentloop.AgentStatusWaitingForMessage {
				t.Fatalf("stopped route admitted work: interrupt=%v running=%v pending=%d status=%s", result.Interrupt, result.State.Running, len(result.State.PendingToolCalls), result.State.Status)
				}
				if result.State.PendingToolsAdmission != (remoteexec.Fence{}) {
					t.Fatal("stopped route retained pending tool admission")
				}
				results := map[conversation.CallID]int{}
				for _, entry := range result.Entries {
					if entry.Kind == conversation.EntryKindToolResult {
						results[entry.Meta.ToolCall.ID]++
						if entry.Meta.ToolExecution == nil || entry.Meta.ToolExecution.ResultCode != toolfailure.InterruptedByUser.String() {
							t.Fatal("pending call did not receive interrupted result")
						}
					}
				}
				for _, call := range calls {
					if results[call.ID] != 1 {
						t.Fatalf("call %s got %d results", call.ID, results[call.ID])
					}
				}
			})
		}
	}
}

func TestStaleFirstRemoteReportAfterIdleStopDexIntegration(t *testing.T) {
	for _, consumer := range []struct {
		name string
		idle bool
	}{{"AwaitRemoteAgent", false}, {"AwaitUser", true}} {
		t.Run(consumer.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			sdk, _, flow, id := stopTestClient(t, ctx)
			defer completeStopTest(t, ctx, sdk, flow, id)
			actor := conversation.Actor{AccountID: "stopper", Email: "stopper@example.com"}
			report := remoteexec.RemoteReport{ConnectionID: "idle-stop", Owner: actor, Epoch: 3, Generation: 5, Sequence: 1,
				ProviderTurn: "first-codex-turn", ToolCall: &remoteexec.RemoteToolCall{Name: conversation.ToolWriteTodos, Arguments: conversation.MustJSONObject(`{"todos":[]}`)}}
			var none dex.None
			if err := sdk.InvokeRPC(ctx, id, flow.InstallRemoteReport, report, &none); err != nil {
				t.Fatal(err)
			}
			var admitted bool
			if err := sdk.InvokeRPC(ctx, id, flow.CommitIdleRemoteStop, actor, &admitted); err != nil {
				t.Fatal(err)
			}
			if !admitted {
				t.Fatal("idle Stop was not admitted")
			}
			before := waitCommittedStop(t, ctx, sdk, flow, id, 0)
			requireOneStop(t, before, actor)
			if before.Executor.Generation != 6 || before.Executor.Remote == nil || before.Executor.Remote.Consumed != 0 || before.State.Turn != 7 || before.Interrupt != nil {
				t.Fatal("idle Stop did not commit generation and marker before report consumption")
			}
			if err := sdk.InvokeRPC(ctx, id, flow.ScheduleStaleRemote, staleRemoteFixture{Boundary: 1, Idle: consumer.idle}, &none); err != nil {
				t.Fatal(err)
			}
			result := waitCommittedStop(t, ctx, sdk, flow, id, 1)
			requireOneStop(t, result, actor)
			if result.Executor.Generation != 6 || result.Executor.Remote == nil || result.Executor.Remote.Consumed != 1 || result.Interrupt != nil || result.State.Turn != 7 || result.State.Running != nil || len(result.State.PendingToolCalls) != 0 {
				t.Fatal("stale report reopened stopped work or failed to advance consumption")
			}
			call := report.Call()
			results, calls := 0, 0
			for _, entry := range result.Entries {
				if entry.Kind == conversation.EntryKindToolResult && entry.Meta.ToolCall.ID == call.ID {
					results++
					if entry.Meta.ToolExecution == nil || entry.Meta.ToolExecution.ResultCode != toolfailure.InterruptedByUser.String() {
						t.Fatal("stale call was not interrupted")
					}
				}
				if entry.Kind == conversation.EntryKindModelReply && len(entry.Meta.ToolCalls) == 1 && entry.Meta.ToolCalls[0].ID == call.ID {
					calls++
				}
			}
			if results != 1 || calls != 1 {
				t.Fatalf("stale call entries: call=%d result=%d, want one each", calls, results)
			}
		})
	}
}
