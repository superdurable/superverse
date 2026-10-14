// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useRef } from "react";
import {
  newPreviewCommand,
  preparePreviewSource,
  refreshAfterRejection,
  type PreviewCommandInput,
} from "@/lib/api/preview";
import { retainedAfterFailure, type PendingCommand } from "@/lib/api/transport/api-error";
import { initialTemplateDesign } from "@/lib/project/initial-template-design";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { preparedSourceOf, readyPreparedSource } from "@/lib/session/session-status";
import { useProjectSessionResource } from "@/lib/session/SessionProvider";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { errorProblem, failureProblem } from "@/lib/shared/problem/problem";
import { projectRemedyLinks } from "@/lib/shared/problem/project-remedies";
import { LATEST_PUSH, versionLabel, versionNumber } from "@/lib/shared/version/version";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Button } from "@/components/ui/Button";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";

/**
 * Prepares the Session's current pushed commit for Preview: its App manifest
 * and Flow Definitions, which the Preview configuration reads. Only this
 * explicit build, an automatic pushed-commit build, or a Preview start wakes a
 * Sandbox. Automatic builds are switched in Project details, which it links to.
 * The parent remounts it for a new source or Sandbox instance, which is a new
 * operation; the same one keeps its request ID for an uncertain acceptance.
 */
export function PreviewSourceControl({ projectId }: { projectId: string }) {
  const { resource, refresh } = useProjectSessionResource();
  const project = useProjectResource()?.snapshot;
  const action = useSingleFlight<"prepare">();
  const operation = useRef<PendingCommand<PreviewCommandInput> | null>(null);
  const source = preparedSourceOf(resource);
  const ready = readyPreparedSource(source, resource?.headCommit) !== null;
  const preparing = source?.status === "PREVIEW_SOURCE_STATUS_PREPARING";
  const active = resource?.status === "SESSION_STATUS_ACTIVE";
  const initialDesign = Boolean(initialTemplateDesign(project, resource));
  const automatic = resource?.agent?.description?.isPreviewAutoDeployEnabled ?? true;
  const failed = source?.status === "PREVIEW_SOURCE_STATUS_FAILED" && source.sourceCommit === resource?.headCommit;
  const name = source?.version ? versionNumber(source.version) : "the latest push";

  const prepare = () =>
    action.run("prepare", async (signal) => {
      if (!active || preparing) return;
      // A settled source answered the earlier request; the next click is a new one.
      if (
        source &&
        source.requestId === operation.current?.input.requestId &&
        (source.status === "PREVIEW_SOURCE_STATUS_READY" || source.status === "PREVIEW_SOURCE_STATUS_FAILED")
      )
        operation.current = null;
      const current = (operation.current ??= newPreviewCommand(projectId, resource?.preview));
      try {
        await preparePreviewSource(current.input, signal);
      } catch (reason) {
        operation.current = retainedAfterFailure(current, reason);
        if (operation.current) throw reason;
        await refreshAfterRejection(refresh, signal, reason, "Building processes is unavailable");
      }
      operation.current = null;
      await refresh(signal, true);
    });

  const operationText = "Building processes";
  const problem = action.failure
    ? errorProblem(action.failure.reason, { operation: operationText })
    : source?.failure
      ? failureProblem(source.failure, {
          operation: operationText,
          commit: source.sourceCommit || resource?.headCommit,
          version: source.version,
        })
      : null;

  return (
    <section aria-label="Build processes" className="border-b border-line-soft bg-panel p-3">
      <div className="flex flex-wrap items-center gap-3 text-xs">
        <span className="text-ink-dim">Build processes</span>
        {/* The Git-owned version names the pushed source without exposing its hash. */}
        <span data-testid="preview-current-commit" className="min-w-0 truncate text-ink">
          {!resource?.headCommit
            ? "Loading the latest push"
            : source?.sourceCommit === resource?.headCommit && source?.version
              ? versionLabel(source.version)
              : LATEST_PUSH}
        </span>
        {(!automatic || failed) && (
          <Button
            size="sm"
            className="ml-auto"
            disabled={!active || action.busy !== null || preparing}
            onClick={() => void prepare()}
          >
            {action.busy !== null || preparing
              ? "Building processes…"
              : failed
                ? source?.version
                  ? `Build ${versionNumber(source.version)} processes again`
                  : "Build processes again"
                : "Build processes"}
          </Button>
        )}
      </div>
      <div className="mt-2 flex flex-wrap items-baseline gap-x-2 text-xs">
        <p role="status" className="text-ink-faint">
          {ready
            ? `Processes of ${name} ready`
            : preparing
              ? `Building ${name} processes…`
              : initialDesign && automatic
                ? "The initial template design is available. The next push builds its processes automatically, or turn off automatic builds to build it now."
                : automatic
                  ? "Each pushed version builds its processes automatically."
                  : "Commit and push workspace changes, then build processes."}
        </p>
        {/* Automatic builds are switched in Project details. */}
        <Link className="text-accent hover:underline" href={studioRoutes.project(projectId)}>
          {automatic ? "Automatic builds are on" : "Automatic builds are off"}
        </Link>
      </div>
      {!ready && problem && (
        <ProblemPanel
          className="mt-2"
          problem={problem}
          actions={{ ...projectRemedyLinks(projectId), FAILURE_REMEDY_APPLY_AGAIN: { onClick: () => void prepare() } }}
        />
      )}
    </section>
  );
}
