// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { createProject } from "@/lib/api/project";
import { readStoredPreference, STORAGE_KEYS, writeStoredPreference } from "@/lib/browser/storage";
import { errorMessage } from "@/lib/api/transport/api-error";
import {
  clearPendingProjectCreation,
  PROJECT_EXECUTOR,
  PROJECT_MESSAGE_DELIVERY,
  encodedPendingProjectCreation,
  forgetStartAttachments,
  readPendingProjectCreation,
  readProjectIntent,
  storeCreationProjectIntent,
  storeProjectIntent,
  releaseProjectIntent,
  type PendingProjectCreation,
  type ProjectIntent,
} from "@/lib/project/project-intents";
import { beginFirstRun } from "@/lib/project/stage/ProjectStageProvider";
import { isAgentModel } from "@/lib/session/agent-types";
import { agentModelInput } from "@/lib/session/model/agent-model";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { StartupSteps } from "@/components/session/start/StartupSteps";
import { Banner } from "@/components/ui/Banner";
import { Button, buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

/**
 * Creates the stored Project with stable request IDs. The Superverse agent starts with its first
 * message and model on the server. A local-agent creation keeps its message in ProjectIntent,
 * opens the accepted Project, and lets its chat pair and send after remote authority is visible. A
 * creation from an example carries the example and no message; what the person typed becomes the new
 * Project's unsent composer draft.
 */
export function ProjectCreationPage() {
  const router = useRouter();
  const request = useRef<PendingProjectCreation | null>(null);
  const inFlight = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  const [projectName, setProjectName] = useState("Your new project");
  const [fromExample, setFromExample] = useState(false);
  // Where Start over and Describe a project lead: the start form of the Teamspace the request names.
  const [startHref, setStartHref] = useState(studioRoutes.home);
  const [error, setError] = useState<string | null>(null);
  const [missingIntent, setMissingIntent] = useState(false);
  const [startingLocal, setStartingLocal] = useState(false);

  const admitProject = useCallback(async () => {
    if (!mounted.current || inFlight.current) return;
    const resumed = readProjectIntent();
    if (resumed) {
      storeProjectIntent(resumed);
      if (readStoredPreference(STORAGE_KEYS.composerDraft(resumed.projectId)) === null)
        writeStoredPreference(STORAGE_KEYS.composerDraft(resumed.projectId), resumed.draft ?? resumed.prompt);
      releaseProjectIntent(resumed.projectId);
      beginFirstRun(resumed.projectId);
      router.replace(studioRoutes.projectSurface(resumed.projectId, "flow"));
      return;
    }
    const encodedIntent = encodedPendingProjectCreation();
    const pending = readPendingProjectCreation();
    request.current = pending;
    if (!pending) {
      setMissingIntent(true);
      return;
    }

    setProjectName(pending.name);
    setFromExample(pending.example !== undefined);
    setStartingLocal(pending.executor === PROJECT_EXECUTOR.LOCAL);
    setStartHref(studioRoutes.projectStart(pending.teamSpaceId));
    setMissingIntent(false);
    const controller = new AbortController();
    inFlight.current = controller;
    const ownsRequest = () =>
      mounted.current &&
      !controller.signal.aborted &&
      inFlight.current === controller &&
      request.current === pending &&
      encodedPendingProjectCreation() === encodedIntent;
    setError(null);
    const attachmentIds = (pending.attachments ?? []).map((attachment) => attachment.attachmentId);
    const sendsMessage =
      pending.executor === PROJECT_EXECUTOR.SUPERVERSE && (pending.prompt !== "" || attachmentIds.length > 0);
    try {
      const projectId = await createProject(
        {
          requestId: pending.idempotencyKey,
          name: pending.name,
          managedRepository: pending.example ? { example: pending.example } : {},
          teamSpaceId: pending.teamSpaceId,
          // A creation from an example carries no message, so no message ID.
          initialMessage: sendsMessage ? pending.prompt : undefined,
          messageId: sendsMessage ? pending.messageIdempotencyKey : undefined,
          // The first message's files, which the start page uploaded; never with an example.
          initialAttachmentIds: sendsMessage ? attachmentIds : [],
          model:
            pending.executor === PROJECT_EXECUTOR.SUPERVERSE && pending.model !== null && isAgentModel(pending.model)
              ? agentModelInput(pending.model)
              : undefined,
        },
        controller.signal,
      );
      if (!ownsRequest()) return;
      // What the person typed before opening an example waits in its composer; nothing sends it.
      if (pending.draft) writeStoredPreference(STORAGE_KEYS.composerDraft(projectId), pending.draft);
      if (pending.executor === PROJECT_EXECUTOR.LOCAL) {
        const intent: ProjectIntent = { ...pending, projectId, delivery: PROJECT_MESSAGE_DELIVERY.UNSENT };
        storeCreationProjectIntent(intent);
        writeStoredPreference(STORAGE_KEYS.composerDraft(projectId), pending.draft ?? pending.prompt);
        releaseProjectIntent(projectId);
      }
      clearPendingProjectCreation();
      // The Project's first message took the start page's files.
      forgetStartAttachments(pending.teamSpaceId);
      // The new Project opens on its first conversation, the chat alone, until the agent's requirements arrive;
      // one created from an example has its brief at once and opens the standard Builder.
      beginFirstRun(projectId);
      router.replace(studioRoutes.projectSurface(projectId, "flow"));
    } catch (reason) {
      if (ownsRequest()) setError(errorMessage(reason, "Could not create project"));
    } finally {
      if (inFlight.current === controller) inFlight.current = null;
    }
  }, [router]);

  useEffect(() => {
    mounted.current = true;
    const start = window.setTimeout(() => void admitProject(), 0);
    return () => {
      mounted.current = false;
      window.clearTimeout(start);
      inFlight.current?.abort();
      inFlight.current = null;
    };
  }, [admitProject]);

  function returnToCreation() {
    inFlight.current?.abort();
    inFlight.current = null;
    if (readPendingProjectCreation()?.idempotencyKey === request.current?.idempotencyKey) clearPendingProjectCreation();
    router.replace(startHref);
  }

  return (
    <main className="flex min-h-0 min-w-0 flex-1 items-center-safe justify-center-safe overflow-y-auto p-8 max-sm:p-4">
      <div className="w-full max-w-2xl">
        <h1 className="text-display font-semibold tracking-tight">{projectName}</h1>

        {!missingIntent && !error && (
          <div className="mt-6 grid justify-items-start gap-2">
            <StartupSteps current="project" mark />
            <p className="text-body text-ink-dim">
              {fromExample
                ? "The project opens on the example as it is built. What you typed waits in its chat, unsent, for you to change it."
                : startingLocal
                  ? "Your description stays in this browser until your local agent connects."
                  : "Your description goes to the agent as soon as the project is ready."}
            </p>
          </div>
        )}
        {!missingIntent && error && (
          <section className="mt-6 rounded-lg border border-line-soft bg-well p-5">
            <h2 className="text-title font-medium text-ink">The project was not created</h2>
            <p className="mt-1 text-reading text-ink-dim">
              Try again. If it keeps failing, start over from the description.
            </p>
          </section>
        )}

        {error && (
          <div className="mt-4">
            <Banner>{error}</Banner>
            <div className="mt-3 flex flex-wrap gap-2">
              <Button onClick={() => void admitProject()}>Try again</Button>
              <Button onClick={returnToCreation}>Start over</Button>
            </div>
          </div>
        )}

        {missingIntent && (
          <section className="mt-6 rounded-lg border border-line-soft bg-panel p-5">
            <h2 className="text-title font-medium">Nothing to set up</h2>
            <p className="mt-2 text-reading text-ink-dim">
              This page sets up a project you just described. Describe one to start.
            </p>
            <Link href={startHref} className={cx(buttonStyle(), "mt-4")}>
              Describe a project
            </Link>
          </section>
        )}
      </div>
    </main>
  );
}
