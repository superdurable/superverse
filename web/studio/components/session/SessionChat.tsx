// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import {
  AgentTasksPanel,
  ConversationView,
  PendingMessageQueue,
  ConfirmedRequirementsView,
  TimerCard,
  TodoProgress,
} from "@/components/agent";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { Button } from "@/components/ui/Button";
import { Spinner } from "@/components/ui/Icons";
import { AgentStatus, hasRunningTasks, hasRunningWork } from "@/lib/session/agent-types";
import { connectorContributionView } from "@/lib/session/agent-view";
import { useMockScenarios } from "@/lib/session/mock-scenarios";
import { useComposerProposal } from "@/lib/session/ComposerDraftProvider";
import { useTemplateCommit } from "@/lib/project/stage/ProjectStageProvider";
import { useProjectSession } from "@/lib/session/SessionProvider";
import { isWorkspacePaused, isWorkspacePreparing, isWorkspaceRetainedDirty } from "@/lib/session/session-status";
import { Composer } from "@/components/session/composer/Composer";
import { MockComposer } from "./mock/MockComposer";
import { PendingActionDock } from "./PendingActionDock";
import {
  composerDisabledReason,
  composerPlaceholder,
  lifecycleMessage,
  mapPendingMessages,
  notificationLinks,
  agentStatusLabel,
  pendingQueueActions,
  shownChatMessages,
} from "./session-chat-state";
import { SessionModelMenu } from "./SessionModelPicker";
import { LiveNote, SeeOnPreview, useLiveChange, type LiveViewing } from "./LiveNote";
import { StartupStatus } from "./start/StartupSteps";
import { useCreationIntent } from "./start/use-creation-intent";
import { useChatSend } from "./start/use-chat-send";
import { useComposerText, useDraftFocus } from "./start/use-first-turn-composer";
import { useStartingModel } from "./start/use-starting-model";
import { RemoteAgentOfflineNotice } from "./executor/SessionExecutorStatus";

const SENDING_FIRST_MESSAGE = "Sending your description…";

/**
 * The Project's Session chat: its conversation, todo progress and queued messages, the pending
 * action dock and the composer. The Project's creation intent, the model and first message the
 * person chose on the creation page, is delivered as the Session's first turn; when that fails,
 * the composer holds the message as a draft for the person to send. While a requirements card
 * waits, a message answers it with the changes the person asks for. The model sits in the ⋯ menu
 * on the composer's row. While the person views Live
 * the chat stays live, and a note above the composer says that what they ask for is built on
 * Preview; once the agent made a change they asked for there, it offers See it on Preview.
 */
export function SessionChat({ live }: { live?: LiveViewing }) {
  const state = useProjectSession();
  const mockScenarios = useMockScenarios();
  const creation = useCreationIntent(useStartingModel(mockScenarios));
  // The creation draft is the first message that could not be delivered.
  const failedDraft = creation.failed ? creation.pending : null;
  const [composerText, setComposerText] = useComposerText(state.projectId, failedDraft?.initialMessage ?? "");
  // A Step, run or item another surface asks about arrives once, as the chip and optional text.
  const { about, clearAbout, focusKey, draft } = useComposerProposal(setComposerText);
  const liveChange = useLiveChange(
    live,
    state.resource?.headCommit ?? "",
    state.snapshot?.description?.status === AgentStatus.WAITING_FOR_MESSAGE,
  );
  const markSent = liveChange.markSent;
  const creationSend = creation.send;
  const send = useChatSend({ send: creationSend, about, clearAbout, markSent });
  const template = useTemplateCommit();
  const shownMessages = useMemo(() => shownChatMessages(state.messages, template), [state.messages, template]);
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
    if (state.historyLoading || !state.hasEarlierMessages) return;
    await state.jumpToBeginning();
    window.requestAnimationFrame(() => {
      const current = scroller.current;
      if (current) current.scrollTop = 0;
    });
  }, [state]);

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
  // A waiting requirements card takes the person's message as their reply; questions are answered first.
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
  // While the creation intent's first message is on its way, the composers wait for it.
  const sendingFirstMessage = creation.pending !== null && !creation.failed ? SENDING_FIRST_MESSAGE : null;
  const disabledReason =
    composerDisabledReason({
      productInteractive,
      lifecycle,
      agentReady,
      snapshotCurrent: state.snapshotCurrent,
      blockedByPending,
      placeholder,
    }) ?? sendingFirstMessage;
  const composerInput = useDraftFocus(failedDraft?.idempotencyKey ?? null, disabledReason);
  // A Session on the mock model takes its scenarios' inputs, answers included, from the mock composer.
  const runsMock = !remotelyExecuted && description?.model === "mock";
  const modelMenu = description && !remotelyExecuted && (
    <SessionModelMenu
      model={description.model}
      contextModel={description.contextModel}
      effort={description.effort}
      mockSelectable={mockScenarios?.selectable ?? false}
      disabled={state.mutating || agentInteractionDisabled}
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
      }) ?? sendingFirstMessage);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <aside className="flex min-h-0 flex-1 flex-col">
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
            {state.hasEarlierMessages && (
              <div className="sticky top-0 z-10 flex flex-wrap items-center justify-center gap-2 rounded-md border border-line-soft bg-bg/95 p-2 backdrop-blur">
                <Button size="sm" disabled={state.historyLoading} onClick={() => void loadEarlierAtTop()}>
                  Load earlier
                </Button>
                <Button
                  variant="quiet"
                  size="sm"
                  disabled={state.historyLoading}
                  onClick={() => void jumpToBeginning()}
                >
                  Jump to beginning
                </Button>
              </div>
            )}
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
            {description?.confirmedRequirements && (
              <ConfirmedRequirementsView confirmed={description.confirmedRequirements} />
            )}
            {pendingTimer && <TimerCard durationSeconds={pendingTimer.durationSeconds} reason={pendingTimer.reason} />}
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
            {creation.failed && (
              <div className="rounded border border-line-soft bg-panel p-3 text-[12px] text-ink-dim" role="alert">
                Your description was not sent. It waits in the composer: check the model in the ⋯ menu, then send it.
              </div>
            )}
          </div>
        </div>

        <PendingActionDock
          approval={pendingApproval}
          input={pendingInput}
          confirmedRevision={description?.confirmedRequirements?.revision ?? 0}
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
          onDecide={(callId, decision) => void state.decideRequirements(callId, decision).catch(() => undefined)}
          onAnswer={(callId, answers) => void state.answerQuestions(callId, answers).catch(() => undefined)}
        />

        {live && <LiveNote live={live} />}

        {runsMock ? (
          <MockComposer
            key={state.projectId}
            catalog={mockScenarios}
            progress={description?.mockProgress ?? null}
            turnRunning={description !== null && description.status !== AgentStatus.WAITING_FOR_MESSAGE}
            tasksRunning={description !== null && hasRunningTasks(description)}
            lastSequence={description?.lastSequence ?? 0}
            pendingInput={pendingInput}
            busy={state.mutating}
            disabledReason={mockDisabledReason}
            initial={creation.pending?.mock ?? null}
            onSend={(text) => {
              markSent();
              return creationSend(text);
            }}
            onDecide={state.decideRequirements}
            onAnswer={state.answerQuestions}
            onStop={canStop ? stop : undefined}
            stopDisabled={state.mutating || !state.snapshotCurrent}
            stopping={state.stopping}
            menu={modelMenu}
          />
        ) : (
          <Composer
            onSend={send}
            inputRef={composerInput}
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
