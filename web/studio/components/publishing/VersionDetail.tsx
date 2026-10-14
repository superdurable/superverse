// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import type { CommitVersion__Output } from "@/lib/generated/protobuf/superverse/v2/CommitVersion";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { ReleaseCandidate__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseCandidate";
import { buildValidationText } from "@/lib/project/setup/preview-validation";
import { useResourceWatch } from "@/lib/project/use-resource-watch";
import { formatMonthDayTime } from "@/lib/shared/format";
import { enumText, timestampToISO } from "@/lib/shared/protobuf";
import { commitDetail, versionLabel, versionNumber } from "@/lib/shared/version/version";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import { openPublishPopover } from "./publish/publish-opener";
import { ReleaseDeployment } from "./ReleaseDeployment";

/** Where a version stands against Live. */
export type VersionPosition = "ahead" | "live" | "behind";

/** The builds of a version, newest first, each with how it met the Preview rule. */
function BuildHistory({ projectId, commit, liveBuild }: { projectId: string; commit: string; liveBuild: string }) {
  const release = useResourceWatch("release", projectId, commit)?.snapshot;
  const builds = (release?.buildRevisions ?? []).toSorted(
    (a, b) => Date.parse(timestampToISO(b.createdAt) ?? "") - Date.parse(timestampToISO(a.createdAt) ?? ""),
  );
  if (builds.length === 0) return <p className="text-[12.5px] text-ink-faint">Never published.</p>;
  return (
    <ul aria-label="Builds" className="grid gap-1.5 text-[12.5px]">
      {builds.map((build) => (
        <li key={build.buildRevisionId} className="rounded-lg bg-well px-2.5 py-1.5" data-testid="build-history">
          <span className="text-ink">
            {formatMonthDayTime(timestampToISO(build.createdAt) ?? "")} ·{" "}
            {enumText(build.status, "RELEASE_STATUS").toLowerCase()}
            {build.buildRevisionId === liveBuild && " · Live runs this build"}
          </span>
          <span className="block text-ink-dim">{buildValidationText(build.previewValidation)}</span>
          <details className="text-[11px] text-ink-faint">
            <summary className="cursor-pointer">Technical details</summary>
            {commitDetail(commit)} · build {build.buildRevisionId} · configuration revision{" "}
            {build.configurationRevision}
          </details>
        </li>
      ))}
    </ul>
  );
}

/**
 * One version of Tools, Publishing: its history of builds, with each one's Preview validation or skip
 * reason, and its action by where it stands: a version ahead of Live opens the Publish popover on it, the
 * Live one opens it on Live's waiting settings, and an older one rolls Live back, after a confirmation.
 */
export function VersionDetail({
  projectId,
  version,
  position,
  between,
  project,
  publishing,
  onRollback,
}: {
  projectId: string;
  /** The commit, with its subject when it is among the listed versions. */
  version: { sourceCommit: string; subject: string; version: CommitVersion__Output | null };
  position: VersionPosition;
  /** The versions between this one and Live, newest first. */
  between: readonly ReleaseCandidate__Output[];
  project: ProjectSnapshot__Output | null;
  /** A publish runs, which a new one waits for. */
  publishing: boolean;
  /** A rollback was admitted; the stage bar follows its deployment. */
  onRollback: (deploymentId: string) => void;
}) {
  const commit = version.sourceCommit;
  const [comparing, setComparing] = useState(false);
  const [rollingBack, setRollingBack] = useState(false);

  return (
    <div className="min-w-0 space-y-3">
      <section
        className="rounded-[10px] border border-line-soft bg-panel px-4 py-3.5"
        aria-label={`Version ${versionNumber(version.version)}`}
      >
        <p className="text-[15px] font-semibold text-ink" title={commitDetail(commit)}>
          {versionNumber(version.version)}
        </p>
        <p className="mt-0.5 text-[13px] text-ink-faint">{version.subject || "Untitled change"}</p>
        <div className="mt-3 flex flex-col items-start gap-1.5">
          {position === "ahead" && (
            <Button disabled={publishing} onClick={() => openPublishPopover({ mode: "version", commit })}>
              <Glyph name="upload" />
              Publish {versionNumber(version.version)}…
            </Button>
          )}
          {position === "live" && (
            <>
              <span className="inline-flex items-center gap-1.5 rounded-full border border-line-soft px-2 py-0.5 text-xs text-ink-dim">
                <span aria-hidden="true" className="size-1.5 rounded-full bg-pass" />
                This is Live
              </span>
              <button
                type="button"
                disabled={publishing}
                onClick={() => openPublishPopover({ mode: "live" })}
                className="text-[13px] text-accent hover:underline disabled:opacity-50"
              >
                Publish its waiting settings…
              </button>
            </>
          )}
          {position === "behind" && (
            <>
              <Button onClick={() => setRollingBack(true)} disabled={rollingBack || publishing}>
                <Glyph name="chevronLeft" className="size-3.5" />
                Roll Live back to {versionNumber(version.version)}…
              </Button>
              <span className="text-[11.5px] text-ink-faint">Live goes back; Preview stays on the latest version.</span>
            </>
          )}
          {position !== "live" && between.length > 0 && (
            <Button size="sm" onClick={() => setComparing((value) => !value)}>
              {comparing ? "Hide changes" : "Compare with Live"}
            </Button>
          )}
        </div>
        {comparing && (
          <div className="mt-2.5 flex flex-col gap-0.5 rounded-lg bg-well px-2.5 py-2 text-[12.5px] text-ink-dim">
            <b className="text-ink">{position === "ahead" ? "What it adds to Live" : "What Live has that it lacks"}</b>
            {between.map((item) => (
              <span key={item.sourceCommit} className="truncate">
                {item.version ? versionLabel(item.version) : item.subject || "Untitled change"}
              </span>
            ))}
          </div>
        )}
      </section>
      <section className="rounded-[10px] border border-line-soft bg-panel px-4 py-3" aria-label="History">
        <h3 className="mb-2 text-[11px] tracking-[0.07em] text-ink-faint uppercase">Builds</h3>
        <BuildHistory
          projectId={projectId}
          commit={commit}
          liveBuild={project?.live?.releaseId === commit ? project.live.buildRevisionId : ""}
        />
      </section>
      {rollingBack && (
        <ReleaseDeployment
          projectId={projectId}
          releaseId={commit}
          project={project}
          publishing={publishing}
          onClose={() => setRollingBack(false)}
          onStarted={(deploymentId) => {
            setRollingBack(false);
            onRollback(deploymentId);
          }}
        />
      )}
    </div>
  );
}
