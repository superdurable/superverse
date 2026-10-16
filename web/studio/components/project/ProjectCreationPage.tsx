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
import { errorMessage } from "@/lib/api/transport/api-error";
import {
  clearPendingProjectCreation,
  encodedPendingProjectCreation,
  readPendingProjectCreation,
  storeProjectIntent,
  type PendingProjectCreation,
} from "@/lib/project/project-intents";
import { beginFirstRun } from "@/lib/project/stage/ProjectStageProvider";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { StartupSteps } from "@/components/session/start/StartupSteps";
import { Banner } from "@/components/ui/Banner";
import { Button, buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

export function ProjectCreationPage() {
  const router = useRouter();
  const request = useRef<PendingProjectCreation | null>(null);
  const inFlight = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  const [projectName, setProjectName] = useState("Your new project");
  // Where Start over and Describe a project lead: the start form of the Teamspace the request names.
  const [startHref, setStartHref] = useState(studioRoutes.home);
  const [error, setError] = useState<string | null>(null);
  const [missingIntent, setMissingIntent] = useState(false);

  const admitProject = useCallback(async () => {
    if (!mounted.current || inFlight.current) return;
    const encodedIntent = encodedPendingProjectCreation();
    const pending = readPendingProjectCreation();
    request.current = pending;
    if (!pending) {
      setMissingIntent(true);
      return;
    }

    setProjectName(pending.name);
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
    try {
      const projectId = await createProject(
        {
          requestId: pending.idempotencyKey,
          name: pending.name,
          managedRepository: {},
          teamSpaceId: pending.teamSpaceId,
        },
        controller.signal,
      );
      if (!ownsRequest()) return;
      // Build applies the chosen model and sends the first message once the Project's Session starts.
      storeProjectIntent(projectId, {
        initialMessage: pending.prompt,
        model: pending.model,
        mock: pending.mock,
        idempotencyKey: pending.messageIdempotencyKey,
      });
      clearPendingProjectCreation();
      // The new Project opens on its first conversation, the chat alone, until the agent's requirements arrive.
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
            <StartupSteps current="project" />
            <p className="text-body text-ink-dim">
              Your description goes to the agent as soon as the project is ready.
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
