// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useId } from "react";
import { updatePreviewAutoDeploy } from "@/lib/api/preview";
import type { PreviewDeploymentSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewDeploymentSnapshot";
import type { PreviewSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewSnapshot";
import { useProjectSessionResource } from "@/lib/session/SessionProvider";
import { LATEST_PUSH, versionLabel } from "@/lib/shared/version/version";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { errorProblem } from "@/lib/shared/problem/problem";
import { projectRemedyLinks } from "@/lib/shared/problem/project-remedies";
import { cx } from "@/components/ui/cx";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";

/**
 * A commit as the panel names it: the version the Preview serves or prepared, and "the latest push"
 * for one it has not prepared yet.
 */
function commitName(preview: PreviewSnapshot__Output | null, commit: string): string {
  if (preview?.sourceCommit === commit && preview.servingVersion) return versionLabel(preview.servingVersion);
  const prepared = preview?.preparedSource;
  if (prepared?.sourceCommit === commit && prepared.version) return versionLabel(prepared.version);
  return LATEST_PUSH;
}

/** Where the Agent's latest push stands beside what the Preview serves. */
function headWords(name: string): string {
  return name === LATEST_PUSH ? "the latest push is not on it yet" : `the latest push is ${name}`;
}

/** What the newest automatic deployment did, in words; nothing before the first one. */
function deploymentText(
  preview: PreviewSnapshot__Output | null,
  deployment: PreviewDeploymentSnapshot__Output,
): string | null {
  const commit = commitName(preview, deployment.commit);
  switch (deployment.status) {
    case "PREVIEW_DEPLOYMENT_STATUS_RUNNING":
      return `Deploying ${commit}…`;
    case "PREVIEW_DEPLOYMENT_STATUS_READY":
      return `Deployed ${commit}.`;
    case "PREVIEW_DEPLOYMENT_STATUS_INCOMPATIBLE":
      return "The pushed version needs configuration before the Preview can run it; the Preview shows what it lacks.";
    case "PREVIEW_DEPLOYMENT_STATUS_FAILED":
      return "Deploying the pushed version failed; the Preview shows why.";
    case "PREVIEW_DEPLOYMENT_STATUS_SKIPPED":
      return `${commit} was not deployed: the workspace was not running.`;
    case "PREVIEW_DEPLOYMENT_STATUS_CANCELLED":
      return `Deploying ${commit} was cancelled.`;
    case "PREVIEW_DEPLOYMENT_STATUS_UNSPECIFIED":
      return null;
  }
}

/**
 * Project details' switch "Build each push automatically" (on for a new Session): after an Agent push
 * the Project builds its processes and deploys them to the Preview by itself; off, a person builds each
 * push with Build processes. It reads the Session snapshot; switching wakes no Sandbox.
 */
export function PreviewAutoDeploy({ projectId }: { projectId: string }) {
  const session = useProjectSessionResource();
  const action = useSingleFlight<"switch">();
  const switchId = useId();
  const description = session.resource?.agent?.description ?? null;
  const isEnabled = description?.isPreviewAutoDeployEnabled ?? false;
  const canSwitch = action.busy === null && description !== null && session.status === "SESSION_STATUS_ACTIVE";

  const toggle = () =>
    action.run("switch", async (signal) => {
      if (!canSwitch) return;
      await updatePreviewAutoDeploy(projectId, !isEnabled, signal);
      await session.refresh(signal, true);
    });

  return (
    <div className="grid gap-1.5 text-[13px]" data-testid="preview-auto-deploy">
      <div className="flex items-center gap-2">
        <button
          id={switchId}
          type="button"
          role="switch"
          aria-checked={isEnabled}
          disabled={!canSwitch}
          onClick={() => void toggle()}
          className={cx(
            "relative h-4 w-7 shrink-0 rounded-full border transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:opacity-50",
            isEnabled ? "border-accent bg-accent" : "border-line bg-well",
          )}
        >
          <span
            aria-hidden="true"
            className={cx(
              "absolute top-0.5 left-0.5 h-2.5 w-2.5 rounded-full bg-panel transition-transform",
              isEnabled && "translate-x-3",
            )}
          />
        </button>
        <label htmlFor={switchId} className="text-ink">
          Build each push automatically
        </label>
      </div>
      <p className="text-[12.5px] text-ink-faint">
        Every push the agent makes is built and put on Preview. Turn it off to build each push by hand with Build
        processes, on the Flow or in Setup.
      </p>
      {action.failure && (
        <ProblemPanel
          problem={errorProblem(action.failure.reason, { operation: "Changing automatic process builds" })}
          actions={{ ...projectRemedyLinks(projectId), FAILURE_REMEDY_APPLY_AGAIN: { onClick: () => void toggle() } }}
        />
      )}
    </div>
  );
}

/**
 * Where the Preview stands beside the Agent's latest push and what its newest automatic deployment did,
 * with whether pushes build by themselves and a link to Project details, where that is switched. The
 * Preview's problem panel shows why a deployment failed and what a refused one lacks.
 */
export function PreviewDeploymentStatus({ projectId }: { projectId: string }) {
  const resource = useProjectSessionResource().resource;
  const preview = resource?.preview ?? null;
  const deployment = preview?.automaticDeployment ?? null;
  const head = resource?.headCommit ?? "";
  const automatic = resource?.agent?.description?.isPreviewAutoDeployEnabled;
  const status = deployment ? deploymentText(preview, deployment) : null;
  const failed = deployment?.status === "PREVIEW_DEPLOYMENT_STATUS_FAILED";
  return (
    <div className="grid gap-1 text-xs">
      <p className="break-all text-ink-faint" data-testid="preview-commit-gap">
        {preview?.sourceCommit
          ? `Preview is at ${commitName(preview, preview.sourceCommit)}`
          : "Preview runs no commit yet"}
        {head && preview?.sourceCommit !== head && `, ${headWords(commitName(preview, head))}`}
        {head && preview?.sourceCommit === head && " (up to date)"}
      </p>
      {status && (
        <p role="status" className={cx(failed ? "text-fail" : "text-ink-dim")}>
          {status}
          {deployment?.queuedCommit && ` ${commitName(preview, deployment.queuedCommit)} waits to deploy next.`}
        </p>
      )}
      {automatic !== undefined && (
        <p className="text-ink-faint">
          {automatic ? "Each push builds and deploys by itself." : "Pushes wait to be built."}{" "}
          <Link className="text-accent hover:underline" href={studioRoutes.project(projectId)}>
            Project details
          </Link>
        </p>
      )}
    </div>
  );
}
