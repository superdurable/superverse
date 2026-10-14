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
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"

	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
)

// These scenarios run background commands, Session notifications, automatic
// turns and the Stop button on an existing READY Project and its actual Worker,
// whose server runs the mock Claude model; Dex, the Sandbox, its command
// ledger and the tools are real. They read the whole retained history, because
// the Snapshot carries only the current window of fewer than 20 entries.

// history is the Session's retained conversation: the archived chunks and the
// Snapshot's current window, in order.
func (s *mockSession) history(ctx context.Context) (agentloop.AgentSnapshot, []conversation.SequencedMessage) {
	s.t.Helper()
	snapshot, err := s.client.GetSnapshot(ctx, s.id)
	if err != nil || snapshot.Agent == nil {
		s.t.Fatalf("reading the Snapshot of %s: %v", s.id, err)
	}
	agent := *snapshot.Agent
	messages := slices.Clone(agent.History.Messages)
	if before := agent.History.NextBeforeSequence; before != nil {
		archived, err := s.client.GetArchivedMessageRange(ctx, s.id, *before, 1000)
		if err != nil {
			s.t.Fatalf("reading the archive of %s: %v", s.id, err)
		}
		messages = append(archived.Messages, messages...)
	}
	return agent, messages
}

// ofRole returns the messages of one role, in order.
func ofRole(messages []conversation.SequencedMessage, role conversation.MessageRole) []conversation.SequencedMessage {
	var selected []conversation.SequencedMessage
	for _, message := range messages {
		if message.Message.Role == role {
			selected = append(selected, message)
		}
	}
	return selected
}

// toolResult returns the one result of a tool, or fails the test.
func toolResult(t *testing.T, messages []conversation.SequencedMessage, name conversation.ToolName, index int) conversation.ToolExecutionMetrics {
	t.Helper()
	var results []conversation.ToolExecutionMetrics
	for _, message := range ofRole(messages, conversation.MessageRoleTool) {
		if *message.Message.ToolName == name && message.Message.ToolExecution != nil {
			results = append(results, *message.Message.ToolExecution)
		}
	}
	if index >= len(results) {
		t.Fatalf("expected a %s result at %d, got %d results", name, index, len(results))
	}
	return results[index]
}

// markers counts the dividers with text in the history.
func markers(messages []conversation.SequencedMessage, text string) int {
	count := 0
	for _, message := range ofRole(messages, conversation.MessageRoleSystem) {
		if message.Message.Content == text {
			count++
		}
	}
	return count
}

// commandEnded waits until the Session records the end of every background
// command it started, and returns them.
func (s *mockSession) commandsEnded(ctx context.Context, count int) []agenttasks.BackgroundCommand {
	s.t.Helper()
	agent := s.waitForSnapshot(ctx, fmt.Sprintf("the end of %d background commands", count), func(agent agentloop.AgentSnapshot) bool {
		commands := agent.Description.BackgroundCommands
		return len(commands) == count && !slices.ContainsFunc(commands, agenttasks.BackgroundCommand.IsRunning)
	})
	return agent.Description.BackgroundCommands
}

// A command started with run_in_background answers its task ID at once and the
// turn ends while it runs. Its exit is recorded and published as a task
// notification, which opens an automatic turn: the model reads the task's
// output with read_task_output from the task ID the notification carries, and
// the mock model goes on only when that output holds the exit and the
// command's last line. The notification is projected as a notification, never
// as a user message.
func TestSessionMockBackgroundCommandDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "background-command", 1).Content)
	session.waitForSnapshot(ctx, "the automatic turn after the notification", func(agent agentloop.AgentSnapshot) bool {
		return len(replies(agent.History)) >= 4 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 4 || !strings.Contains(assistant[3].Message.Content, "printed background-done") {
		t.Fatalf("expected the 4 mock replies ending with the output; got %d: %+v", len(assistant), assistant)
	}
	if started := toolResult(t, messages, conversation.ToolBash, 0); started.Outcome != conversation.ToolOutcomeSucceeded || started.ResultCode != "" {
		t.Fatalf("the background start is not a successful result: %+v", started)
	}
	if read := toolResult(t, messages, conversation.ToolReadTaskOutput, 0); read.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("read_task_output did not succeed: %+v", read)
	}
	notifications := ofRole(messages, conversation.MessageRoleNotification)
	if len(notifications) != 1 {
		t.Fatalf("expected one notification, got %d", len(notifications))
	}
	notification := notifications[0].Message.Notification
	commands := agent.Description.BackgroundCommands
	if len(commands) != 1 || notification == nil || notification.Kind != conversation.NotificationKindTask || notification.Source != conversation.NotificationSourceCommand ||
		notification.TaskID != commands[0].TaskID || notification.TaskStatus != string(agenttasks.BackgroundCommandExited) || notification.ID != "task/"+commands[0].TaskID+"/exited" ||
		notifications[0].Message.Content != notification.Summary || !strings.Contains(notification.Summary, "exited with code 0") {
		t.Fatalf("the notification does not report the command's exit: %+v, %+v", notifications[0].Message, commands)
	}
	if notifications[0].Sequence < assistant[1].Sequence || notifications[0].Sequence > assistant[2].Sequence {
		t.Fatal("the notification is not between the first turn's end and the automatic turn")
	}
	command := commands[0]
	if command.Status != agenttasks.BackgroundCommandExited || command.EndReason != agenttasks.BackgroundCommandEndExit || command.ExitCode == nil || *command.ExitCode != 0 ||
		command.OutputBytes != int64(len("background-done\n")) || command.Description != "Build in the background" {
		t.Fatalf("the background command's record is wrong: %+v", command)
	}
}

// A foreground command that runs past its timeout moves to the background
// instead of being stopped: the model reads its output so far and its task ID
// in a successful result. When the command exits, its notification opens an
// automatic turn that reads the end of its output, which the mock model
// requires.
func TestSessionMockCommandTimeoutDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "command-timeout", 1).Content)
	session.waitForSnapshot(ctx, "the automatic turn after the command's end", func(agent agentloop.AgentSnapshot) bool {
		return len(replies(agent.History)) >= 4 && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	assistant := ofRole(messages, conversation.MessageRoleAssistant)
	if len(assistant) != 4 || !strings.Contains(assistant[3].Message.Content, "printed numbering-finished") {
		t.Fatalf("expected the 4 mock replies ending with the command's last line; a stopped scenario answers with one text: %+v", assistant)
	}
	for _, reply := range assistant {
		requireMock(t, reply, mockInputTokens)
	}
	moved := toolResult(t, messages, conversation.ToolBash, 0)
	if moved.Outcome != conversation.ToolOutcomeSucceeded || moved.ResultCode != "" || moved.ExitCode != nil {
		t.Fatalf("the command past its timeout did not move to the background: %+v", moved)
	}
	if duration := moved.DurationMS; duration == nil || *duration <= 0 || *duration >= (25*time.Second).Milliseconds() {
		t.Fatalf("the moved command's result was not recorded at its timeout: %v", duration)
	}
	commands := agent.Description.BackgroundCommands
	if len(commands) != 1 || commands[0].Status != agenttasks.BackgroundCommandExited || commands[0].ExitCode == nil || *commands[0].ExitCode != 0 ||
		commands[0].OutputBytes != 40_000+int64(len("numbering-finished\n")) {
		t.Fatalf("the moved command's record is wrong: %+v", commands)
	}
	if notifications := ofRole(messages, conversation.MessageRoleNotification); len(notifications) != 1 || notifications[0].Message.Notification.TaskID != commands[0].TaskID {
		t.Fatalf("expected the moved command's one notification: %+v", notifications)
	}
}

// A long turn beside a background command: the turn's 60 progress-list updates
// move the Session to a new run while the command sleeps for seven minutes, and
// the turn still ends before the command, because the Session only waits for the
// BackgroundCommandFlow SubFlow that watches it. The wait is carried into the new
// run, so the command's exit later records its end and opens the automatic turn
// that reads its output, which the mock model requires.
func TestSessionMockBackgroundCommandNewRunDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "background-long-turn", 1).Content)
	session.waitForSnapshot(ctx, "the background command's start", func(agent agentloop.AgentSnapshot) bool {
		return len(agent.Description.BackgroundCommands) == 1
	})
	startRun := currentRun(t, ctx, session.reader, string(session.id))
	turnEnd := session.waitForSnapshot(ctx, "the end of the long turn", func(agent agentloop.AgentSnapshot) bool {
		return lastReplyContains(agent, "I will read it when it finishes")
	})
	if commands := turnEnd.Description.BackgroundCommands; len(commands) != 1 || !commands[0].IsRunning() {
		t.Fatalf("the long turn did not end while the background command ran: %+v", commands)
	}
	if run := currentRun(t, ctx, session.reader, string(session.id)); run == startRun {
		t.Fatal("the long turn did not move the Session to a new run, so the test proves nothing; the scenario must make more Steps")
	}
	watches := backgroundCommandWatches(t, ctx, session.reader, session.id)
	if len(watches) != 1 {
		t.Fatalf("expected one AwaitBackgroundCommand waiting for its SubFlow, got %v", watches)
	}
	watch := watches[0]
	if status := session.execution(ctx, watch).GetFlowStatus(); status != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		t.Fatalf("the SubFlow %s that watches the running command is %s", watch, status)
	}
	session.waitForSnapshot(ctx, "the automatic turn after the command's exit", func(agent agentloop.AgentSnapshot) bool {
		return lastReplyContains(agent, "printed long-build-done")
	})
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	command := agent.Description.BackgroundCommands[0]
	if command.Status != agenttasks.BackgroundCommandExited || command.ExitCode == nil || *command.ExitCode != 0 || command.OutputBytes != int64(len("long-build-done\n")) {
		t.Fatalf("the background command's record is wrong: %+v", command)
	}
	if notifications := ofRole(messages, conversation.MessageRoleNotification); len(notifications) != 1 || notifications[0].Message.Notification.TaskID != command.TaskID {
		t.Fatalf("expected the command's one notification: %+v", notifications)
	}
	if status := session.execution(ctx, watch).GetFlowStatus(); status != dexpb.FlowStatus_FLOW_STATUS_COMPLETED {
		t.Fatalf("the SubFlow %s that watched the command is %s, not completed", watch, status)
	}
	t.Logf("FlowID=%s moved on from run %s while SubFlow %s watched %s", session.id, startRun, watch, command.TaskID)
}

// lastReplyContains reports whether the agent waits for a message after a
// reply that holds text.
func lastReplyContains(agent agentloop.AgentSnapshot, text string) bool {
	assistant := replies(agent.History)
	return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && len(assistant) > 0 && strings.Contains(assistant[len(assistant)-1].Message.Content, text)
}

// backgroundCommandWatches are the FlowIDs of the SubFlows that the waiting
// AwaitBackgroundCommand Steps of Session flowID started, from a bounded
// diagnostic read of the engine state; Dex derives each from the Session and
// the Step execution.
func backgroundCommandWatches(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID agentloop.FlowID) []string {
	t.Helper()
	state, err := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(flowID)})
	if err != nil {
		t.Fatalf("bounded engine state read failed (%T)", err)
	}
	var watches []string
	for _, step := range state.GetActiveStepExecutions() {
		if step.GetStepType() == dex.GetFinalStepType(sessionagent.AwaitBackgroundCommand{}) && len(step.GetWaitingCondition().GetSubFlowConditions()) == 1 {
			watches = append(watches, fmt.Sprintf("SubFlow:%s-%s-0", flowID, step.GetStepExecutionId()))
		}
	}
	return watches
}

// Stop during a running turn: a background server runs and a long foreground
// check is in flight. A Stop that names an earlier turn is refused; the Stop
// of the running turn ends it at once, answers the check interrupted_by_user,
// stops the server, which sends no notification, and pauses automatic turns.
// A Stop with nothing running is refused, and the scenario's input 2, the
// user's next message, continues from the interrupted result.
func TestSessionMockStopDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "stop-turn", 1).Content)
	running := session.waitForSnapshot(ctx, "the long check in flight", func(agent agentloop.AgentSnapshot) bool {
		commands := agent.Description.BackgroundCommands
		return len(replies(agent.History)) == 2 && agent.Description.Status != agentloop.AgentStatusWaitingForMessage &&
			len(commands) == 1 && commands[0].IsRunning()
	})
	turn := running.Description.Turn
	// The Snapshot shows no tool in flight before it commits; the check sleeps for minutes, so it
	// runs by now, and the Stop cancels it rather than meeting it at a boundary.
	time.Sleep(10 * time.Second)
	stale := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: turn - 1})
	if !errors.Is(stale, sessionagent.ErrCommandRejected) {
		t.Fatalf("a Stop of an earlier turn was not refused: %v", stale)
	}
	if err := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: turn}); err != nil {
		t.Fatalf("the Stop of turn %d was not admitted: %v", turn, err)
	}
	// A second press joins the pending Stop, or finds the turn already ended; either way one
	// InterruptAgent ends the turn, which the single Stopped marker below shows.
	if repeated := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: turn}); repeated != nil && !errors.Is(repeated, sessionagent.ErrCommandRejected) {
		t.Fatalf("the repeated Stop failed: %v", repeated)
	}
	commands := session.commandsEnded(ctx, 1)
	if commands[0].Status != agenttasks.BackgroundCommandStopped || commands[0].EndReason != agenttasks.BackgroundCommandEndStopped {
		t.Fatalf("the background server was not stopped: %+v", commands[0])
	}
	stopped := session.waitForSnapshot(ctx, "the end of the stopped turn", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	if stopped.Description.Turn != turn {
		t.Fatalf("the Stop started a turn: %d after %d", stopped.Description.Turn, turn)
	}
	// A stopped command sends no notification; give its record's watch time to publish one.
	time.Sleep(15 * time.Second)
	_, messages := session.history(ctx)
	if notifications := ofRole(messages, conversation.MessageRoleNotification); len(notifications) != 0 {
		t.Fatalf("a stopped command sent a notification: %+v", notifications)
	}
	check := toolResult(t, messages, conversation.ToolBash, 1)
	if check.ResultCode != toolfailure.InterruptedByUser.String() || check.Outcome != conversation.ToolOutcomeUnknown {
		t.Fatalf("the check in flight was not answered interrupted_by_user with an unknown effect: %+v", check)
	}
	if stops, pauses := markers(messages, agentloop.StoppedMarkerForTest), markers(messages, agentloop.PausedMarkerForTest); stops != 1 || pauses != 1 {
		t.Fatalf("the Stop left %d Stopped by user and %d paused markers, not one each", stops, pauses)
	}
	if idle := session.client.InterruptAgent(ctx, session.id, agentloop.InterruptAgentRequest{Turn: turn}); !errors.Is(idle, sessionagent.ErrCommandRejected) {
		t.Fatalf("a Stop with nothing running was not refused: %v", idle)
	}
	after := stopped.Description.LastSequence
	if err := session.client.SendPendingMessage(ctx, session.id, conversation.PendingUserMessage{MessageID: conversation.MessageID(fmt.Sprintf("continue-%d", time.Now().UnixNano())), Value: conversation.UserMessage{Content: mockInput(t, "stop-turn", 2).Content}}); err != nil {
		t.Fatal(err)
	}
	continued := session.waitForSnapshot(ctx, "the reply after the Stop", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, after)
	})
	final := replies(continued.History)
	if reply := final[len(final)-1].Message.Content; !strings.Contains(reply, "stopped before it finished") {
		t.Fatalf("the next turn did not read the interrupted result: %q", reply)
	}
}

// The automatic-turn bound: each background command's notification opens a
// turn that starts the next command, until agent.taskTurns (5 by
// default) consecutive automatic turns ran; the next notification is only
// recorded with the one-time paused marker.
func TestSessionMockAutomaticTurnLimitDexIntegration(t *testing.T) {
	const taskTurns = 5
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "background-chain", 1).Content)
	session.commandsEnded(ctx, taskTurns+1)
	session.waitForSnapshot(ctx, "the paused automatic continuation", func(agent agentloop.AgentSnapshot) bool {
		return agent.Description.Status == agentloop.AgentStatusWaitingForMessage && slices.ContainsFunc(agent.History.Messages, func(message conversation.SequencedMessage) bool {
			return message.Message.Role == conversation.MessageRoleSystem && message.Message.Content == agentloop.PausedMarkerForTest
		})
	})
	time.Sleep(15 * time.Second)
	agent, messages := session.history(ctx)
	requireNoFailure(t, conversation.HistoryPage{Messages: messages})
	if replies := ofRole(messages, conversation.MessageRoleAssistant); len(replies) != 2*(taskTurns+1) {
		t.Fatalf("expected %d replies, from the user's turn and %d automatic turns; got %d", 2*(taskTurns+1), taskTurns, len(replies))
	}
	if notifications := ofRole(messages, conversation.MessageRoleNotification); len(notifications) != taskTurns+1 {
		t.Fatalf("expected %d notifications, got %d", taskTurns+1, len(notifications))
	}
	if len(agent.Description.BackgroundCommands) != taskTurns+1 || agent.Description.Turn != taskTurns+1 {
		t.Fatalf("expected %d commands and turns, got %d commands at turn %d", taskTurns+1, len(agent.Description.BackgroundCommands), agent.Description.Turn)
	}
	paused := 0
	for _, message := range ofRole(messages, conversation.MessageRoleSystem) {
		if message.Message.Content == agentloop.PausedMarkerForTest {
			paused++
		}
	}
	if paused != 1 {
		t.Fatalf("the paused marker was recorded %d times", paused)
	}
}

// The task tools: read_task_output reads a running background command's state
// and output, and stop_task stops it, which sends no notification.
func TestSessionMockTaskToolsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "task-tools", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	if assistant := replies(agent.History); len(assistant) != 4 || !strings.Contains(assistant[3].Message.Content, "was stopped") {
		t.Fatalf("expected the 4 mock replies; a stopped scenario answers with one text: %+v", assistant)
	}
	for _, tool := range []conversation.ToolName{conversation.ToolBash, conversation.ToolReadTaskOutput, conversation.ToolStopTask} {
		if result := toolResult(t, agent.History.Messages, tool, 0); result.Outcome != conversation.ToolOutcomeSucceeded {
			t.Fatalf("%s did not succeed: %+v", tool, result)
		}
	}
	commands := session.commandsEnded(ctx, 1)
	if commands[0].Status != agenttasks.BackgroundCommandStopped {
		t.Fatalf("stop_task did not stop the command: %+v", commands[0])
	}
	time.Sleep(15 * time.Second)
	if _, messages := session.history(ctx); len(ofRole(messages, conversation.MessageRoleNotification)) != 0 {
		t.Fatal("a command that stop_task stopped sent a notification")
	}
}

// Admission of notifications from outside the Session: while a requirements
// card waits, nothing is consumed, so 32 distinct notifications are admitted,
// a repeated ID is admitted again without a second publish, and a 33rd
// distinct one is refused.
func TestSessionNotificationAdmissionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "requirements-card", 1).Content)
	session.requirementsCard(ctx, 0)
	notification := func(index int) conversation.SessionNotification {
		taskID := fmt.Sprintf("cmd-admission%03d", index)
		return conversation.SessionNotification{ID: "task/" + taskID + "/exited", Kind: conversation.NotificationKindTask, Source: conversation.NotificationSourceCommand,
			OccurredAt: time.Now().UTC(), TaskID: taskID, TaskStatus: string(agenttasks.BackgroundCommandExited), Summary: "Admission check."}
	}
	report := func(index int) bool {
		admitted, err := session.client.ReportSessionNotification(ctx, session.id, notification(index))
		if err != nil {
			t.Fatalf("reporting notification %d: %v", index, err)
		}
		return admitted
	}
	for index := range agentloop.MaximumPendingNotificationsForTest {
		if !report(index) {
			t.Fatalf("notification %d of %d was refused", index+1, agentloop.MaximumPendingNotificationsForTest)
		}
	}
	if !report(0) {
		t.Fatal("a repeated notification ID was not answered as admitted")
	}
	if report(agentloop.MaximumPendingNotificationsForTest) {
		t.Fatal("a notification beyond the pending bound was admitted")
	}
	invalid := notification(99)
	invalid.Summary = ""
	if _, err := session.client.ReportSessionNotification(ctx, session.id, invalid); err == nil {
		t.Fatal("a task notification without a summary was admitted")
	}
}
