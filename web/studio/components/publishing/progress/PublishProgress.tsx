// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import type { ProjectLive__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectLive";
import { publishSpan, publishStages, type PublishOutcome } from "@/lib/project/publishing/publish-stages";
import { useBuildLog } from "@/lib/project/publishing/use-build-log";
import type { PublishProgress as Progress } from "@/lib/project/publishing/use-publish-progress";
import { formatElapsedDuration, formatTimeAgo } from "@/lib/shared/format";
import { failureProblem } from "@/lib/shared/problem/problem";
import { projectRemedyLinks } from "@/lib/shared/problem/project-remedies";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { versionNumber } from "@/lib/shared/version/version";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";
import { CancelPublish } from "./CancelPublish";
import { PublishLog } from "./PublishLog";
import { StageBar } from "./StageBar";

/** The header of each kind of publish, by its outcome. */
function heading(kind: "version" | "settings" | "rollback", outcome: PublishOutcome, version: string): string {
  if (outcome === "cancelled" || outcome === "cancelling")
    return outcome === "cancelled" ? "Publishing cancelled" : `Cancelling publishing ${version}…`;
  if (kind === "rollback")
    return {
      running: `Rolling Live back to ${version}`,
      published: `Rolled Live back to ${version}`,
      failed: `Live didn't go back to ${version}`,
    }[outcome];
  const what = kind === "settings" ? `${version}'s new settings` : version;
  return { running: `Publishing ${what}`, published: `Published ${what}`, failed: `${what} didn't go live` }[outcome];
}

/**
 * The publish in progress, or the last one: its header with who started it and Cancel, the stage bar, a
 * failure's problem under the bar, and "View logs" / "Hide logs" with the log inline. A cancel or a
 * failure says that Live still runs what it ran.
 */
export function PublishProgress({
  projectId,
  progress,
  live,
  actor,
  onPublishAgain,
}: {
  projectId: string;
  progress: Progress;
  live: ProjectLive__Output | null;
  /** Who started it, from Live's history; empty when unknown. */
  actor: string;
  onPublishAgain: () => void;
}) {
  const [logs, setLogs] = useState(false);
  // Ages are read when the card renders.
  const [now] = useState(() => Date.now());
  const { facts, outcome } = progress;
  const build = facts?.build ?? null;
  const log = useBuildLog(
    build && progress.releaseId
      ? { projectId, releaseId: progress.releaseId, buildRevisionId: build.buildRevisionId }
      : null,
    logs,
  );
  if (!facts || !outcome) return null;
  const version = versionNumber(progress.version, "this version");
  const liveVersion = live ? versionNumber(live.version) : "";
  const running = outcome === "running" || outcome === "cancelling";
  const took = publishSpan(facts);
  const by = actor ? ` by ${actor}` : "";
  const ago = formatTimeAgo(progress.startedAt, now);
  const sub =
    outcome === "running" || outcome === "cancelling"
      ? `Started ${ago || "just now"}${by}`
      : outcome === "published"
        ? `${ago}${by}${took ? ` · took ${formatElapsedDuration(took)}` : ""}`
        : liveVersion
          ? `Live still runs ${liveVersion}`
          : "Live is not published yet";
  const failure =
    outcome === "failed" ? (build?.failure ?? build?.autoDeployFailure ?? facts.deployment?.failure ?? null) : null;
  return (
    <section
      aria-label="Publishing progress"
      className="overflow-hidden rounded-xl border border-line-soft bg-panel"
      data-testid="publish-progress"
      data-outcome={outcome}
    >
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1.5 px-4 pt-3 pb-2.5 text-[13px] text-ink-dim">
        <h3 className="text-[15px] font-semibold text-ink" role="status">
          {heading(facts.kind, outcome, version)}
        </h3>
        <span>{sub}</span>
        {running && (
          <CancelPublish
            projectId={projectId}
            releaseId={progress.releaseId}
            facts={facts}
            version={version}
            liveVersion={liveVersion}
          />
        )}
        <Button
          size="sm"
          variant="quiet"
          className="ml-auto"
          aria-expanded={logs}
          onClick={() => setLogs((value) => !value)}
        >
          {logs ? "Hide logs" : "View logs"}
          <Glyph name="chevronDown" className={cx("size-3.5 transition-transform", logs && "rotate-180")} />
        </Button>
      </div>
      <StageBar stages={publishStages(facts)} label={heading(facts.kind, outcome, version)} />
      {failure && (
        <ProblemPanel
          className="mx-4 mb-3.5"
          problem={failureProblem(failure, {
            operation: facts.kind === "rollback" ? "Rolling Live back" : "Publishing this version",
            version: progress.version,
            commit: progress.releaseId,
          })}
          actions={{
            ...projectRemedyLinks(projectId, studioRoutes.setup(projectId, "live")),
            FAILURE_REMEDY_PUBLISH_AGAIN: { onClick: onPublishAgain },
            FAILURE_REMEDY_APPLY_AGAIN: { onClick: onPublishAgain },
          }}
        />
      )}
      {logs && <PublishLog facts={facts} log={log} running={running} liveVersion={liveVersion || "nothing"} />}
      {progress.disconnected && (
        <p className="px-4 pb-2 text-[12px] text-ink-faint">Updates disconnected. Reconnecting…</p>
      )}
    </section>
  );
}
