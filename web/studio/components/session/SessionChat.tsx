// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { AgentTasksPanel, ConversationView, PendingMessageQueue, TimerCard, TodoProgress } from "@/components/agent";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { Spinner } from "@/components/ui/Icons";
import { AgentStatus, hasRunningTasks, hasRunningWork } from "@/lib/session/agent-types";
import { connectorContributionView } from "@/lib/session/agent-view";
import { useMockScenarios } from "@/lib/session/mock-scenarios";
import { useComposerProposal } from "@/lib/session/ComposerDraftProvider";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { useTemplateCommit } from "@/lib/project/stage/ProjectStageProvider";
import { useProjectSession } from "@/lib/session/SessionProvider";
import { isWorkspacePaused, isWorkspacePreparing, isWorkspaceRetainedDirty } from "@/lib/session/session-status";
import { Composer } from "@/components/session/composer/Composer";
import { MockComposer } from "./mock/MockComposer";
import { HistoryJumps } from "./HistoryJumps";
import { PendingActionDock } from "./PendingActionDock";
import {
  EXAMPLE_GREETING_SEQUENCE,
  composerDisabledReason,
  composerPlaceholder,
  lifecycleMessage,
  mapPendingMessages,
  notificationLinks,
  agentStatusLabel,
  pendingQueueActions,
  shownChatMessages,
} from "./session-chat-state";
import { SessionAgentPicker } from "./executor/SessionAgentPicker";
import { LiveNote, SeeOnPreview, useLiveChange, type LiveViewing } from "./LiveNote";
import { StartupStatus } from "./start/StartupSteps";
import { useChatSend } from "./start/use-chat-send";
import { useComposerText } from "./start/use-first-turn-composer";
import { RemoteAgentOfflineNotice } from "./executor/SessionAgentPicker";

const FIRST_MESSAGE_WAITS = "Your description waits for the agent…";

/**
 * The Project's Session chat: its conversation, todo progress and queued messages, the pending
 * action dock and the composer. CreateProject carries a Superverse agent's first message; a local
 * agent's first message arrives after remote activation. Until the agent takes the message the
 * composers wait. A message is how the person changes what the agent builds; a requirements card a
 * Session still waits at from before cards stopped waiting takes it as its reply. The model sits in
 * the Agent picker on the composer's row. While the person views Live
 * the chat stays live, and a note above the composer says that what they ask for is built on
 * Preview; once the agent made a change they asked for there, it offers See it on Preview.
 */
export function SessionChat({ canManage, live }: { canManage: boolean; live?: LiveViewing }) {
  const state = useProjectSession();
  const mockScenarios = useMockScenarios();
  const [composerText, setComposerText] = useComposerText(state.projectId);
  // A Step, run or item another surface asks about arrives once, as the chip and optional text.
  const { about, clearAbout, focusKey, draft } = useComposerProposal(setComposerText);
  const liveChange = useLiveChange(
    live,
    state.resource?.headCommit ?? "",
    state.snapshot?.description?.status === AgentStatus.WAITING_FOR_MESSAGE,
  );
  const markSent = liveChange.markSent;
  const sendMessage = state.send;
  const send = useChatSend({ send: sendMessage, about, clearAbout, markSent });
  const template = useTemplateCommit();
  // An example the server matched to the first message greets with nothing and records no version 1 of its own.
  const startedFromExample =
    Boolean(useProjectResource()?.snapshot?.example) &&
    state.snapshot?.description?.startingExample?.choice !== "matched";
  const shownMessages = useMemo(
    () => shownChatMessages(state.messages, template, startedFromExample),
    [state.messages, template, startedFromExample],
  );
  const scroller = useRef<HTMLDivElement | null>(null);
  const followsTail = useRef(true);

  const loadEarlierAtTop = useCallback(async () => {
    const element = scroller.current;
    if (!element || state.historyLoading || !state.hasEarlierMessages) return;
    const previousHeight = element.scrollHeight;
    await state.loadEarlier();
    window.requestAnimationFrame(() => {
      const current = scroller.current;
      if (current) current.scrollTop += current.scrollHeight - previousHeight;
    });
  }, [state]);

  const jumpToBeginning = useCallback(async () => {
    if (state.historyLoading) return;
    followsTail.current = false;
    if (state.hasEarlierMessages) await state.jumpToBeginning();
    window.requestAnimationFrame(() => {
      const current = scroller.current;
      if (current) current.scrollTop = 0;
    });
  }, [state]);

  const jumpToLatest = useCallback(() => {
    const element = scroller.current;
    if (!element) return;
    followsTail.current = true;
    element.scrollTo({ top: element.scrollHeight, behavior: "smooth" });
  }, []);

  useEffect(() => {
    const element = scroller.current;
    if (
      element &&
      !state.historyLoading &&
      state.hasEarlierMessages &&
      element.scrollHeight <= element.clientHeight + 1
    ) {
      void loadEarlierAtTop();
    }
  }, [loadEarlierAtTop, state.hasEarlierMessages, state.historyLoading]);

  useEffect(() => {
    const element = scroller.current;
    if (element && followsTail.current) element.scrollTop = element.scrollHeight;
  }, [
    state.messages,
    state.activities,
    state.assistant,
    state.reasoning,
    state.snapshot?.description?.todoList,
    state.snapshot?.description?.confirmedRequirements,
    state.snapshot?.description?.pendingUserInput,
    state.snapshot?.queued,
    state.snapshot?.steered,
  ]);

  const sessionStatus = state.status;
  const productInteractive = sessionStatus === "SESSION_STATUS_ACTIVE";
  const lifecycle = lifecycleMessage(sessionStatus);
  const workspacePreparing = state.resource !== null && isWorkspacePreparing(state.resource);
  const workspaceRetainedDirty = state.resource !== null && isWorkspaceRetainedDirty(state.resource);
  const workspacePaused = state.resource !== null && isWorkspacePaused(state.resource);
  const contributions = useMemo(
    () => (state.resource?.connectorContributions ?? []).map(connectorContributionView),
    [state.resource],
  );
  const description = state.snapshot?.description ?? null;
  const remotelyExecuted = description?.executor?.mode === "remote";
  const pendingInput = description?.pendingUserInput ?? null;
  const pendingApproval = description?.pendingApproval ?? null;
  const pendingTimer = description?.pendingTimer ?? null;
  const agentReady = description !== null && description.status !== AgentStatus.INITIALIZING;
  const agentInteractionDisabled = !productInteractive || !agentReady || !state.snapshotCurrent;
  // Stop shows while a turn or a background task runs; it stops all of them.
  const canStop = productInteractive && agentReady && description !== null && hasRunningWork(description);
  const stopCommand = state.stop;
  const stop = useCallback(() => stopCommand().catch(() => undefined), [stopCommand]);
  const answerAwaitingReconciliation = pendingInput !== null && state.answeredQuestionCallId === pendingInput.callId;
  // An old waiting requirements card takes the person's message as its reply; questions are answered first.
  const blockedByPending =
    pendingInput !== null && (pendingInput.kind !== "requirements" || answerAwaitingReconciliation);
  const submittingQuestionAnswers = pendingInput !== null && state.answeringQuestionCallId === pendingInput.callId;
  const pendingMessages = mapPendingMessages(state.snapshot?.queued ?? [], state.snapshot?.steered ?? []);
  const linksOfNotification = useMemo(() => notificationLinks(state.projectId), [state.projectId]);
  const pendingWaits = useMemo(
    () =>
      [
        pendingApproval && {
          kind: "approval" as const,
          callId: pendingApproval.callId,
          startedAt: pendingApproval.startedAt ?? null,
        },
        pendingInput && {
          kind: "question" as const,
          callId: pendingInput.callId,
          startedAt: pendingInput.startedAt ?? null,
        },
        pendingTimer && {
          kind: "timer" as const,
          callId: pendingTimer.callId,
          startedAt: pendingTimer.startedAt ?? null,
        },
      ].filter((value) => value !== null),
    [pendingApproval, pendingInput, pendingTimer],
  );
  const placeholder = composerPlaceholder({
    pendingInputKind: pendingInput?.kind ?? null,
    answerAwaitingReconciliation,
    sessionStatus,
    viewingLive: live !== undefined,
  });
  // Until the agent takes the first message, which the Session started with, the composers wait for it.
  const firstMessageWaits =
    description !== null && description.lastSequence === 0 && (state.snapshot?.queued.length ?? 0) > 0
      ? FIRST_MESSAGE_WAITS
      : null;
  const disabledReason =
    composerDisabledReason({
      productInteractive,
      lifecycle,
      agentReady,
      snapshotCurrent: state.snapshotCurrent,
      blockedByPending,
      placeholder,
    }) ?? firstMessageWaits;
  // A Session on the mock model takes its scenarios' inputs, answers included, from the mock composer.
  const runsMock = !remotelyExecuted && description?.model === "mock";
  const modelMenu = description && (
    <SessionAgentPicker
      key={`${state.projectId}:${state.resource?.agent?.description?.executor?.epoch}:${state.resource?.agent?.description?.executor?.generation}:${state.status}:${canManage}`}
      model={description.model}
      contextModel={description.contextModel}
      effort={description.effort}
      mockSelectable={mockScenarios?.selectable ?? false}
      canManage={canManage}
      modelDisabled={state.mutating || agentInteractionDisabled}
      onSelect={(model) => void state.updateModel(model, description.modelRevision).catch(() => undefined)}
    />
  );
  const mockDisabledReason = answerAwaitingReconciliation
    ? placeholder
    : (composerDisabledReason({
        productInteractive,
        lifecycle,
        agentReady,
        snapshotCurrent: state.snapshotCurrent,
        blockedByPending: false,
        placeholder,
      }) ?? firstMessageWaits);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <aside className="flex min-h-0 flex-1 flex-col">
        <div className="relative flex min-h-0 flex-1 flex-col">
          <div
            ref={scroller}
            onScroll={() => {
              const element = scroller.current;
              if (element) {
                followsTail.current = element.scrollHeight - element.scrollTop - element.clientHeight < 80;
                if (element.scrollTop < 80) void loadEarlierAtTop();
              }
            }}
            className="min-h-32 flex-1 overflow-y-auto px-4 py-5"
          >
            <div className="flex flex-col gap-4">
              <RemoteAgentOfflineNotice />
              {state.historyLoading && (
                <div className="flex items-center justify-center gap-2 text-[11px] text-ink-faint">
                  <Spinner className="h-3 w-3" />
                  {state.historyLoadMode === "beginning"
                    ? `Loading from beginning… ${state.historyLoadedCount} loaded · more remain`
                    : "Loading earlier messages…"}
                </div>
              )}
              <ConversationView
                key={state.projectId}
                messages={shownMessages}
                reasoning={state.reasoning}
                activities={state.activities}
                assistant={state.assistant}
                pendingWaits={pendingWaits}
                isModelRunning={description?.status === AgentStatus.CALLING_MODEL}
                isExecutionLive={productInteractive}
                streamState={productInteractive && state.assistant?.isComplete === false ? "streaming" : "complete"}
                notificationLinks={linksOfNotification}
                confirmedRequirements={description?.confirmedRequirements ?? null}
                waitingRequirementsCallId={pendingInput?.kind === "requirements" ? pendingInput.callId : null}
                requirementsStartSequence={startedFromExample ? EXAMPLE_GREETING_SEQUENCE : null}
              />
              {live && liveChange.offered && <SeeOnPreview live={live} onFollow={liveChange.dismiss} />}
              {description?.todoList && <TodoProgress items={description.todoList.items} />}
              {description && (
                <AgentTasksPanel
                  commands={description.backgroundCommands}
                  subagents={description.subagents}
                  subagentLines={state.subagentLines}
                  contributions={contributions}
                  stopping={state.stopping}
                />
              )}
              {pendingTimer && (
                <TimerCard durationSeconds={pendingTimer.durationSeconds} reason={pendingTimer.reason} />
              )}
              <PendingMessageQueue
                items={pendingMessages}
                disabled={state.mutating || agentInteractionDisabled}
                {...pendingQueueActions(state.snapshot?.queued ?? [], state, setComposerText)}
              />
              <StartupStatus
                messages={shownMessages}
                description={description}
                lifecycle={productInteractive ? null : lifecycle}
                workspacePreparing={workspacePreparing}
              />
              {workspacePaused && (
                <div
                  className="rounded border border-line-soft bg-panel p-3 text-[12px] text-ink-dim"
                  data-workspace-paused
                  role="status"
                >
                  Paused; your files are kept. The agent&apos;s next change starts the workspace again.
                </div>
              )}
              {workspaceRetainedDirty && (
                <div className="rounded border border-line-soft bg-panel p-3 text-[12px] text-ink-dim">
                  The workspace was kept because it has changes that are not committed or checked yet.
                </div>
              )}
              {productInteractive && description?.status !== AgentStatus.WAITING_FOR_MESSAGE && (
                <div className="flex items-center gap-2 text-caption text-ink-faint" role="status">
                  <Spinner className="h-3.5 w-3.5" /> {agentStatusLabel(description?.status)}
                </div>
              )}
              {state.error && <p className="text-[12px] text-fail">{state.error}</p>}
              {state.commandError && <p className="text-[12px] text-fail">{state.commandError}</p>}
            </div>
          </div>
          <HistoryJumps
            scroller={scroller}
            earlier={state.hasEarlierMessages}
            loading={state.historyLoading}
            onBeginning={jumpToBeginning}
            onLatest={jumpToLatest}
          />
        </div>

        <PendingActionDock
          approval={pendingApproval}
          input={pendingInput}
          currentRevision={description?.confirmedRequirements?.revision ?? 0}
          disabled={state.mutating || agentInteractionDisabled}
          approving={state.mutationKind === "approval"}
          answering={submittingQuestionAnswers}
          answerAwaitingReconciliation={answerAwaitingReconciliation}
          answeredBy={
            !remotelyExecuted && description?.contextModel === "mock"
              ? "On the mock model, the mock composer below sends the answer."
              : undefined
          }
          onApprove={(callId, approved) => void state.approveTool(callId, approved).catch(() => undefined)}
          onAnswer={(callId, answers) => void state.answerQuestions(callId, answers).catch(() => undefined)}
        />

        {live && <LiveNote live={live} />}

        {runsMock ? (
          <MockComposer
            key={state.projectId}
            catalog={mockScenarios}
            progress={description?.mockProgress ?? null}
            turnRunning={description !== null && description.status !== AgentStatus.WAITING_FOR_MESSAGE}
            timerWait={description?.status === "waiting_for_timer"}
            tasksRunning={description !== null && hasRunningTasks(description)}
            lastSequence={description?.lastSequence ?? 0}
            pendingInput={pendingInput}
            busy={state.mutating}
            disabledReason={mockDisabledReason}
            onSend={(text) => {
              markSent();
              return sendMessage(text);
            }}
            onSteer={(text) => {
              markSent();
              return state.change(text, true);
            }}
            onAnswer={state.answerQuestions}
            onStop={canStop ? stop : undefined}
            stopDisabled={state.mutating || !state.snapshotCurrent}
            stopping={state.stopping}
            menu={modelMenu}
          />
        ) : (
          <Composer
            onSend={send}
            about={about}
            onClearAbout={clearAbout}
            focusKey={focusKey + (draft?.focusRequests ?? 0)}
            picking={draft?.picking}
            onPick={draft ? () => draft.setPicking(!draft.picking) : undefined}
            value={composerText}
            onChange={setComposerText}
            busy={state.mutating}
            disabledReason={disabledReason}
            placeholder={placeholder}
            onStop={canStop ? stop : undefined}
            stopDisabled={state.mutating || !state.snapshotCurrent}
            stopping={state.stopping}
            menu={modelMenu}
          />
        )}
      </aside>
    </div>
  );
}
