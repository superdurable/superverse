// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState, type ReactNode } from "react";
import type { CommitVersion__Output } from "@/lib/generated/protobuf/superverse/v2/CommitVersion";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import { buildValidationText } from "@/lib/project/setup/preview-validation";
import { useResourceWatch } from "@/lib/project/use-resource-watch";
import { enumText, timestampToISO } from "@/lib/shared/protobuf";
import { commitDetail, versionNumber } from "@/lib/shared/version/version";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import { cx } from "@/components/ui/cx";
import { HistoryList, HistoryRow } from "./HistoryRows";
import { ReleaseDeployment } from "./ReleaseDeployment";

/** Where a version stands against Live. */
export type VersionPosition = "ahead" | "live" | "behind";

/** The builds of a version, newest first, one line each, opening to how it met the Preview rule. */
function BuildHistory({
  projectId,
  commit,
  name,
  liveBuild,
  action,
}: {
  projectId: string;
  commit: string;
  /** The version, "V12". */
  name: string;
  liveBuild: string;
  action?: ReactNode;
}) {
  // Ages are read when the list first renders.
  const [now] = useState(() => Date.now());
  const release = useResourceWatch("release", projectId, commit)?.snapshot;
  const builds = (release?.buildRevisions ?? []).toSorted(
    (a, b) => Date.parse(timestampToISO(b.createdAt) ?? "") - Date.parse(timestampToISO(a.createdAt) ?? ""),
  );
  return (
    <HistoryList label="History" heading={`Builds · ${name}`} action={action}>
      {builds.length === 0 ? (
        <p className="px-3 py-2.5 text-[13px] text-ink-faint">Never published.</p>
      ) : (
        <ul aria-label="Builds" className="divide-y divide-line-faint">
          {builds.map((build) => (
            <li key={build.buildRevisionId} data-testid="build-history">
              <HistoryRow
                at={timestampToISO(build.createdAt)}
                now={now}
                detail={
                  <>
                    <span className="text-ink-dim">{buildValidationText(build.previewValidation)}</span>
                    <span>
                      {commitDetail(commit)} · build {build.buildRevisionId} · configuration revision{" "}
                      {build.configurationRevision}
                    </span>
                  </>
                }
              >
                <span className="text-ink">Build {enumText(build.status, "RELEASE_STATUS").toLowerCase()}</span>
                {build.buildRevisionId === liveBuild && <span className="text-ink-dim"> · Live runs this build</span>}
              </HistoryRow>
            </li>
          ))}
        </ul>
      )}
    </HistoryList>
  );
}

/** A version's mark in the list and on its card: Preview's, hollow, or Live's, green. */
export function VersionTag({ live, children }: { live?: boolean; children: string }) {
  return (
    <span className="inline-flex h-4.5 shrink-0 items-center gap-1 rounded-full border border-line-soft px-1.5 text-[10.5px] font-normal text-ink-dim">
      <span aria-hidden="true" className={cx("size-1.5 rounded-full", live ? "bg-pass" : "border border-ink")} />
      {children}
    </span>
  );
}

/**
 * One version of Tools, Publishing: its builds, each opening to its Preview validation or skip reason. The
 * version list names and marks it; the bar's Publish publishes. A version older than Live offers to roll Live
 * back beside its builds' heading, after a confirmation.
 */
export function VersionDetail({
  projectId,
  version,
  position,
  project,
  publishing,
  onRollback,
}: {
  projectId: string;
  /** The commit, with its subject when it is among the listed versions. */
  version: { sourceCommit: string; subject: string; version: CommitVersion__Output | null };
  position: VersionPosition;
  project: ProjectSnapshot__Output | null;
  /** A publish runs, which a rollback waits for. */
  publishing: boolean;
  /** A rollback was admitted; the stage bar follows its deployment. */
  onRollback: (deploymentId: string) => void;
}) {
  const commit = version.sourceCommit;
  const name = versionNumber(version.version);
  const [rollingBack, setRollingBack] = useState(false);

  return (
    <div className="min-w-0 space-y-4">
      <BuildHistory
        projectId={projectId}
        commit={commit}
        name={name}
        liveBuild={project?.live?.releaseId === commit ? project.live.buildRevisionId : ""}
        action={
          position === "behind" && (
            <Button
              size="sm"
              onClick={() => setRollingBack(true)}
              disabled={rollingBack || publishing}
              title="Live goes back; Preview stays on the latest version."
            >
              <Glyph name="chevronLeft" className="size-3.5" />
              Roll Live back to {name}…
            </Button>
          )
        }
      />
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
