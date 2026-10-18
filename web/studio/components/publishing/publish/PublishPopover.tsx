// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useEffect, useState, type RefObject } from "react";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/browser/storage";
import type { PreviewStartRecord__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewStartRecord";
import type { ProjectLive__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectLive";
import { usePublishCheck } from "@/lib/project/publishing/use-publish-check";
import { liveDifferenceCount } from "@/lib/project/setup/configuration-items";
import {
  SKIP_REASON_LIMIT,
  skipReasonProblem,
  trimmedSkipReason,
  versionValidation,
} from "@/lib/project/setup/preview-validation";
import type { ReleaseVersions } from "@/lib/project/use-release-versions";
import { errorProblem } from "@/lib/shared/problem/problem";
import { versionNumber } from "@/lib/shared/version/version";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { Spinner } from "@/components/ui/Icons";
import { PopoverCard } from "@/components/ui/Popover";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";
import { RunningMark } from "@/components/ui/wait/LongWait";
import { ConsequenceSummary } from "./ConsequenceSummary";
import { publishProblem, usePublish } from "./publish-commands";
import type { PublishOpening } from "./publish-opener";
import { PublishChecks } from "./PublishChecks";
import { VersionChoice } from "./VersionChoice";

const SECTION = "px-1 pt-2.5 pb-1 text-[11px] tracking-[0.07em] text-ink-faint uppercase";
const NOTE = "mx-1 my-1.5 rounded-lg bg-well px-2.5 py-2 text-[12.5px] text-ink-dim";

/**
 * The reason typed for publishing without a Preview run, kept for this tab in session storage so closing
 * the popover keeps it until it is sent; storage the browser refuses keeps it for this page only.
 */
function useSkipReason(projectId: string): [string, (reason: string) => void] {
  const [reason, setReason] = useState("");
  const key = STORAGE_KEYS.publishSkipReason(projectId);
  useEffect(() => {
    try {
      const stored = readStored("session", key);
      // Read once after mount, so the server's render and the first browser render agree.
      if (stored) queueMicrotask(() => setReason(stored));
    } catch {
      // Blocked storage leaves the reason to this page.
    }
  }, [key]);
  const update = (next: string) => {
    setReason(next);
    try {
      writeStored("session", key, next);
    } catch {
      // Blocked storage leaves the reason to this page.
    }
  };
  return [reason, update];
}

/**
 * The Publish popover: on a version, the version (others folded), what blocks it with a link to each
 * Setup row, what deserves a look, what happens, a required short reason when the version is not in the
 * Preview record, and Publish; on Live's own version (`{ mode: "live" }`, or when no version is ahead),
 * its waiting settings and the newer versions to switch to. Publish builds the frozen copy and starts the build; once admitted the
 * popover closes and `onAccepted` follows it.
 */
export function PublishPopover({
  projectId,
  anchorRef,
  opening,
  onOpening,
  onClose,
  onAccepted,
  versions,
  live,
  previewSource,
  starts,
  links,
}: {
  projectId: string;
  anchorRef: RefObject<HTMLElement | null>;
  opening: PublishOpening;
  onOpening: (opening: PublishOpening) => void;
  onClose: () => void;
  onAccepted: (commit: string, buildRevisionId: string) => void;
  versions: ReleaseVersions;
  live: ProjectLive__Output | null;
  /** The commit of the Preview's READY prepared source; empty without one. */
  previewSource: string;
  starts: readonly PreviewStartRecord__Output[];
  links: { publishing: string; setup: (anchor: string) => string; preview: string };
}) {
  const { ahead } = versions;
  // With no version ahead, Live's own version is what there is to publish: its waiting settings.
  const liveMode = live !== null && (opening.mode === "live" || (versions.loaded && ahead.length === 0));
  const named = opening.mode === "version" ? opening.commit : undefined;
  const commit = liveMode
    ? live.releaseId
    : ((ahead.find((item) => item.sourceCommit === named) ?? ahead[0])?.sourceCommit ?? null);
  const check = usePublishCheck(projectId, commit, previewSource);
  const publish = usePublish(projectId);
  const [reason, setReason] = useSkipReason(projectId);
  const { refresh } = check;
  useEffect(() => {
    if (publish.moved) refresh();
  }, [publish.moved, refresh]);

  const configuration = check.configuration;
  const version = liveMode ? live.version : (ahead.find((item) => item.sourceCommit === commit)?.version ?? null);
  const needsReason = !liveMode && commit !== null && !versionValidation(starts, commit).validated;
  const reasonProblem = needsReason ? skipReasonProblem(reason) : "";
  const ready = configuration?.status === "CONFIGURATION_STATUS_READY";
  const waiting = configuration ? liveDifferenceCount(configuration) : 0;
  const preparing = check.preparation.state === "preparing" || check.preparation.state === "reading";
  const blocked = !configuration || !ready || Boolean(reasonProblem) || !check.release || publish.busy;
  const go = () => {
    if (!configuration || !commit || blocked) return;
    void publish.publish(
      {
        commit,
        nextPublishRevision: configuration.nextPublishRevision,
        previewConfigurationRevision: configuration.previewConfigurationRevision,
        expectedLiveRevision: live?.revision ?? "0",
        previewSkipReason: needsReason ? trimmedSkipReason(reason) : "",
        shown: configuration,
      },
      (buildRevisionId) => {
        if (needsReason) setReason("");
        onAccepted(commit, buildRevisionId);
      },
    );
  };

  return (
    <PopoverCard
      anchorRef={anchorRef}
      align="end"
      label="Publish to Live"
      autoFocus
      tabIndex={-1}
      className="w-[420px] max-w-[calc(100vw-16px)]"
    >
      <div className="grid px-1 pt-1 pb-0.5" data-testid="publish-popover">
        <div className="flex items-center justify-between px-1 pb-2">
          <h2 className="text-[15px] font-semibold text-ink">Publish to Live</h2>
          <Link
            href={links.publishing}
            onClick={onClose}
            className="inline-flex items-center gap-1 text-[12.5px] text-accent hover:underline"
          >
            All versions <Glyph name="external" className="size-3.5" />
          </Link>
        </div>
        {!versions.loaded && !liveMode ? (
          <p className={NOTE} role="status">
            Reading the versions…
          </p>
        ) : liveMode ? (
          <>
            {!configuration && (
              <p className="px-1 pb-1.5 text-[12.5px] text-ink-dim" role="status">
                Reading Live&apos;s waiting settings…
              </p>
            )}
            {ahead.length > 0 && (
              <section aria-label="Newer on Preview">
                <h3 className={SECTION}>Newer on Preview</h3>
                {ahead.slice(0, 3).map((item, index) => (
                  <Link
                    key={item.sourceCommit}
                    href={links.preview}
                    onClick={() => onOpening({ mode: "version", commit: item.sourceCommit })}
                    className="flex items-center gap-2 rounded-md px-1.5 py-1.5 text-[12.5px] hover:bg-hover"
                  >
                    <span className="min-w-0 flex-1 truncate text-ink">
                      <b className="font-semibold">{versionNumber(item.version)}</b>
                      {index === 0 && " · latest"} <span className="text-ink-faint">· {item.subject}</span>
                    </span>
                    <span className="shrink-0 text-ink-faint">Switch ›</span>
                  </Link>
                ))}
              </section>
            )}
          </>
        ) : commit === null ? (
          <p className={NOTE}>
            {versions.error || "Nothing to publish yet. The agent's first change makes the first version."}
          </p>
        ) : (
          <VersionChoice
            ahead={ahead}
            chosen={commit}
            starts={starts}
            onChoose={(next) => onOpening({ mode: "version", commit: next })}
          />
        )}
        {commit !== null && (
          <>
            {check.preparation.state === "failed" ? (
              <ProblemPanel
                className="mx-1 mt-2"
                problem={publishProblem(new Error(check.preparation.error), commit, check.preparation.failure)}
              />
            ) : check.error ? (
              <ProblemPanel
                className="mx-1 mt-2"
                problem={errorProblem(check.error, { operation: "Checking the next publish" })}
              />
            ) : !configuration ? (
              <div className={cx(NOTE, "flex items-center gap-2.5")} role="status">
                {/* The mark comes with the "few minutes" words, so nothing moves when reading turns into preparing;
                    its spinner holds the box until the wait runs long. */}
                {preparing && <RunningMark size="line" />}
                <p>
                  {preparing
                    ? `Getting ${versionNumber(version)} ready to check. The first time takes a few minutes.`
                    : "Checking Live's next publish…"}
                </p>
              </div>
            ) : (
              <>
                <ConsequenceSummary
                  projectId={projectId}
                  commit={commit}
                  configuration={configuration}
                  live={live}
                  settingsOnly={liveMode}
                />
                <PublishChecks configuration={configuration} setupHref={links.setup} onNavigate={onClose} />
                {preparing && (
                  <p className="px-1 pt-1.5 text-[12px] text-ink-faint" role="status">
                    Getting {versionNumber(version)} ready to publish. The first time takes a few minutes.
                  </p>
                )}
              </>
            )}
            {needsReason && (
              <label className="mx-1 mt-1.5 grid gap-1 text-[12.5px] text-ink-dim">
                {versionNumber(version)} is not in the Preview record. Why publish it without a Preview run?
                <textarea
                  className="min-h-14 rounded-md border border-line-soft bg-panel px-2 py-1.5 text-[13px] text-ink focus-visible:outline-2 focus-visible:outline-accent"
                  value={reason}
                  required
                  aria-invalid={Boolean(reasonProblem) && reason !== ""}
                  onChange={(event) => setReason(event.target.value)}
                  placeholder="For example: it ran on Preview yesterday, before the record was cleared"
                />
                <span className={cx("text-[11.5px]", reason && reasonProblem ? "text-fail" : "text-ink-faint")}>
                  {reason && reasonProblem ? reasonProblem : `Required, at most ${SKIP_REASON_LIMIT} characters.`}
                </span>
              </label>
            )}
            {publish.moved && (
              <p className="mx-1 mt-2 text-[12.5px] text-busy" role="status" data-testid="publish-moved">
                {publish.moved}
              </p>
            )}
            {publish.failure && (
              <ProblemPanel
                className="mx-1 mt-2"
                problem={publishProblem(publish.failure.reason, commit)}
                actions={{
                  FAILURE_REMEDY_APPLY_AGAIN: { onClick: go },
                  FAILURE_REMEDY_PUBLISH_AGAIN: { onClick: go },
                }}
              />
            )}
            <div className="mt-2 flex items-center justify-end gap-2 border-t border-line-faint px-1 pt-2">
              <Button
                variant="primary"
                disabled={blocked}
                title={
                  !ready && configuration
                    ? "Set up what Live needs first"
                    : reasonProblem
                      ? "Say why it goes without a Preview run"
                      : preparing
                        ? "Getting this version ready to publish"
                        : undefined
                }
                data-revision={configuration?.nextPublishRevision}
                data-preview-revision={configuration?.previewConfigurationRevision}
                onClick={go}
              >
                {publish.busy ? <Spinner className="size-3.5" /> : <Glyph name="upload" />}
                {publish.retrying
                  ? "Retry publishing"
                  : liveMode
                    ? waiting === 0
                      ? `Publish ${versionNumber(version)} again`
                      : waiting === 1
                        ? "Publish this change"
                        : "Publish these changes"
                    : `Publish ${versionNumber(version)}`}
              </Button>
            </div>
          </>
        )}
      </div>
    </PopoverCard>
  );
}
