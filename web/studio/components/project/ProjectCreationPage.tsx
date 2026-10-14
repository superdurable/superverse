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
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Banner } from "@/components/ui/Banner";
import { Button, buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { Spinner } from "@/components/ui/Icons";

export function ProjectCreationPage() {
  const router = useRouter();
  const request = useRef<PendingProjectCreation | null>(null);
  const inFlight = useRef<AbortController | null>(null);
  const mounted = useRef(false);
  const [projectName, setProjectName] = useState("Starting your project");
  // Where Back to New project leads: the start form of the TeamSpace the request names.
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
        <Eyebrow>Project lifecycle</Eyebrow>
        <h1 className="mt-2 text-3xl font-semibold tracking-tight">{projectName}</h1>

        {!missingIntent && (
          <section className="mt-6 rounded-lg border border-line-soft bg-well p-5">
            <div className="flex items-center gap-3">
              {!error && <Spinner className="h-4 w-4" />}
              <div>
                <h2 className="font-medium text-ink">
                  {error ? "Project admission needs another attempt" : "Admitting project"}
                </h2>
                <p className="mt-1 text-sm text-ink-dim">
                  {error
                    ? "Retry if the provider failure was temporary, or return to start a new request."
                    : "Securing the project identity before repository provisioning begins…"}
                </p>
              </div>
            </div>
          </section>
        )}

        {error && (
          <div className="mt-4">
            <Banner>{error}</Banner>
            <div className="mt-3 flex flex-wrap gap-2">
              <Button onClick={() => void admitProject()}>Retry project admission</Button>
              <Button onClick={returnToCreation}>Back to project creation</Button>
            </div>
          </div>
        )}

        {missingIntent && (
          <section className="mt-6 rounded-lg border border-line-soft bg-panel p-5">
            <h2 className="font-medium">No pending project request</h2>
            <p className="mt-2 text-sm text-ink-dim">
              Start a new project so Studio has a name and initial request to submit.
            </p>
            <Link href={startHref} className={cx(buttonStyle(), "mt-4")}>
              Back to New project
            </Link>
          </section>
        )}
      </div>
    </main>
  );
}
