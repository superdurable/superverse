// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { sendMessage } from "@/lib/api/agent";
import { getSession } from "@/lib/api/session";
import { ApiError } from "@/lib/api/transport/api-error";
import { watch, WatchConnectionError } from "@/lib/api/transport/watch";
import { useResourceWatch } from "@/lib/project/use-resource-watch";
import {
  clearProjectIntent,
  PROJECT_MESSAGE_DELIVERY,
  storeProjectIntent,
  type ProjectIntent,
} from "@/lib/project/project-intents";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import { commandFailureText } from "@/lib/session/session-commands";
import { delay, isAbortError } from "@/lib/shared/async";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { StartupSteps } from "@/components/session/start/StartupSteps";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { ProjectCreationPairing } from "./ProjectCreationPairing";

const SESSION_START_TIMEOUT = 30_000;
const SESSION_START_RETRY = 1_000;
const WATCH_RETRY = 750;

/** A Project Watch gates Session reads; a Session Watch gates the first local message on remote authority. */
export function ProjectCreationLocal({
  intent,
  onDone,
}: {
  intent: ProjectIntent;
  onDone: (projectId: string) => void;
}) {
  const project = useResourceWatch("project", intent.projectId, intent.projectId);
  const ready = project?.snapshot?.status === "PROJECT_STATUS_READY";
  const [session, setSession] = useState<SessionSnapshot__Output | null>(null);
  const [error, setError] = useState("");
  const [sendError, setSendError] = useState("");
  const [delivery, setDelivery] = useState(intent.delivery);
  const sending = useRef(false);
  const sendRequest = useRef<AbortController | null>(null);
  useEffect(() => () => sendRequest.current?.abort(), []);

  useEffect(() => {
    if (!ready) return;
    const controller = new AbortController();
    const signal = controller.signal;
    void (async () => {
      const deadline = Date.now() + SESSION_START_TIMEOUT;
      while (!signal.aborted) {
        try {
          const current = await getSession({ projectId: intent.projectId }, signal);
          if (current.projectId !== intent.projectId) throw new Error("Session snapshot scope mismatch");
          setSession(current);
          break;
        } catch (reason) {
          if (signal.aborted) return;
          if (!(reason instanceof ApiError && reason.status === 404 && Date.now() < deadline)) throw reason;
          await delay(SESSION_START_RETRY, signal);
        }
      }
      let cursor = "";
      while (!signal.aborted) {
        try {
          await watch(
            "WatchSession",
            { projectId: intent.projectId },
            {
              resumeCursor: cursor,
              signal,
              onFrame: (frame) => {
                cursor = frame.resumeCursor;
                if (!frame.snapshot) return;
                if (frame.snapshot.projectId !== intent.projectId) throw new Error("Session Watch scope mismatch");
                setSession(frame.snapshot);
              },
            },
          );
        } catch (reason) {
          if (signal.aborted) return;
          if (!(reason instanceof WatchConnectionError) || !reason.retryable) throw reason;
          await delay(WATCH_RETRY, signal);
        }
      }
    })().catch((reason: unknown) => {
      if (!isAbortError(reason) && !signal.aborted)
        setError(reason instanceof Error ? reason.message : "Could not watch the Project's agent");
    });
    return () => controller.abort();
  }, [intent.projectId, ready]);

  const active = session?.status === "SESSION_STATUS_ACTIVE";
  const remote = session?.agent?.description?.executor?.mode === "SESSION_EXECUTOR_MODE_REMOTE";
  const alreadyQueued =
    session?.agent?.queued.some((message) => message.messageId === intent.messageIdempotencyKey) ||
    session?.agent?.steered.some((message) => message.messageId === intent.messageIdempotencyKey);
  useEffect(() => {
    if (!active || !remote) return;
    // An example opens with no first message: once the local agent holds the Session, the Project opens.
    if (!intent.prompt) {
      if (sending.current) return;
      sending.current = true;
      clearProjectIntent(intent.projectId);
      onDone(intent.projectId);
      return;
    }
    if (delivery === PROJECT_MESSAGE_DELIVERY.SENDING) {
      if (alreadyQueued && !sending.current) {
        sending.current = true;
        clearProjectIntent(intent.projectId);
        onDone(intent.projectId);
      }
      return;
    }
    if (sending.current || sendError) return;
    sending.current = true;
    const controller = new AbortController();
    sendRequest.current = controller;
    void (async () => {
      // Persist the uncertain boundary before calling a command that does not deduplicate message IDs.
      try {
        storeProjectIntent({ ...intent, delivery: PROJECT_MESSAGE_DELIVERY.SENDING });
        setDelivery(PROJECT_MESSAGE_DELIVERY.SENDING);
      } catch (reason) {
        sending.current = false;
        setSendError(reason instanceof Error ? reason.message : "Could not save the first message before sending");
        return;
      }
      await sendMessage(
        {
          projectId: intent.projectId,
          messageId: intent.messageIdempotencyKey,
          text: intent.prompt,
          attachmentIds: [],
        },
        controller.signal,
      );
      if (controller.signal.aborted) return;
      clearProjectIntent(intent.projectId);
      onDone(intent.projectId);
    })().catch((reason: unknown) => {
      if (controller.signal.aborted) return;
      const definite =
        reason instanceof ApiError &&
        (reason.errorInfo?.reason === "AGENT_COMMAND_NOT_ADMITTED" ||
          reason.errorInfo?.reason === "AGENT_COMMAND_NOT_DISPATCHED");
      if (definite) {
        try {
          storeProjectIntent({ ...intent, delivery: PROJECT_MESSAGE_DELIVERY.UNSENT });
          setDelivery(PROJECT_MESSAGE_DELIVERY.UNSENT);
          sending.current = false;
        } catch (storageError) {
          setSendError(
            storageError instanceof Error ? storageError.message : "Could not save the first message for retry",
          );
          return;
        }
      }
      setSendError(commandFailureText(reason));
    });
  }, [active, alreadyQueued, delivery, intent, onDone, remote, sendError]);

  if (project?.snapshot?.status === "PROJECT_STATUS_SETUP_FAILED" || project?.snapshot?.sessionStartFailure)
    return (
      <Banner className="mt-6">Project setup or Session start failed. Open the Project to inspect its status.</Banner>
    );
  if (project?.error || error) return <Banner className="mt-6">{project?.error || error}</Banner>;
  return (
    <div className="mt-6 grid justify-items-start gap-3">
      {!active && <StartupSteps current={!ready ? "project" : "agent"} mark />}
      {!active ? (
        <p className="text-body text-ink-dim">
          Preparing the Project and its agent. Your description is saved in this browser.
        </p>
      ) : remote ? (
        <div className="grid gap-3">
          <p className="text-body text-ink-dim" role="status">
            {delivery === PROJECT_MESSAGE_DELIVERY.SENDING && !alreadyQueued
              ? "The first message may already be with your local agent. It will not be sent again automatically."
              : intent.prompt
                ? "Your local agent is connected. Sending your first message…"
                : "Your local agent is connected. Opening the example…"}
          </p>
          {sendError && <Banner>{sendError}</Banner>}
          {sendError && delivery === PROJECT_MESSAGE_DELIVERY.UNSENT && (
            <Button onClick={() => setSendError("")}>Try sending again</Button>
          )}
          {(sendError || delivery === PROJECT_MESSAGE_DELIVERY.SENDING) && (
            <>
              <div className="rounded-lg border border-line-soft bg-panel p-3 text-sm text-ink-dim">
                <p className="font-medium text-ink">Your original message</p>
                <p className="mt-1 whitespace-pre-wrap">{intent.prompt}</p>
              </div>
              <Link
                href={studioRoutes.projectSurface(intent.projectId, "flow")}
                onClick={() => clearProjectIntent(intent.projectId)}
                className="text-sm text-accent underline"
              >
                Open Project conversation and check before sending again
              </Link>
            </>
          )}
        </div>
      ) : (
        <ProjectCreationPairing projectId={intent.projectId} />
      )}
    </div>
  );
}
