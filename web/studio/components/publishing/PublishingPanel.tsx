// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { listReleases } from "@/lib/api/release";
import type { CommitVersion__Output } from "@/lib/generated/protobuf/superverse/v2/CommitVersion";
import type { ReleaseSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseSnapshot";
import { usePublishProgress, type PublishFocus } from "@/lib/project/publishing/use-publish-progress";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { useReleaseVersions } from "@/lib/project/use-release-versions";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { usePagedList } from "@/lib/browser/use-paged-list";
import { useQueryUpdate } from "@/lib/browser/use-query-update";
import { formatTimeAgo } from "@/lib/shared/format";
import { enumText, timestampToISO } from "@/lib/shared/protobuf";
import { commitDetail, versionNumber } from "@/lib/shared/version/version";
import { LiveDeployment } from "./LiveDeployment";
import { LiveHistory, useLiveHistory } from "./LiveHistory";
import { Notice, RequestProblem } from "./RequestProblem";
import { VersionDetail, VersionTag, type VersionPosition } from "./VersionDetail";
import { openPublishPopover } from "./publish/publish-opener";
import { PublishProgress } from "./progress/PublishProgress";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

const RELEASE_ID_COMMIT = /^[a-f0-9]{40}$/;

/** A row of the version list: a main-branch commit, or an older Release beyond the newest hundred. */
interface VersionRow {
  sourceCommit: string;
  subject: string;
  /** The version Git gives the commit, null for a Release prepared before versions were recorded. */
  version: CommitVersion__Output | null;
  committedAt: string | null;
  release: ReleaseSnapshot__Output | null;
}

/** The publish the URL names: the build a publish just started, or the deployment of a rollback. */
function focusOf(query: URLSearchParams): PublishFocus | null {
  const build = query.get("build");
  const release = query.get("release");
  const deployment = query.get("deployment");
  if (build && release && RELEASE_ID_COMMIT.test(release)) return { releaseId: release, buildRevisionId: build };
  return deployment ? { deploymentId: deployment } : null;
}

/**
 * Tools, Publishing: the publish in progress or the last one, with its stage bar, Cancel and log; what is
 * Live with its address and Stop Live; every version newest first, Preview's and Live's marked, with its
 * builds; then Live's history. The bar's Publish publishes; a version older than Live rolls Live back after
 * a confirmation.
 */
export function PublishingPanel({ projectId }: { projectId: string }) {
  const query = useSearchParams();
  const { queryHref, replaceQuery } = useQueryUpdate();
  const [refresh, setRefresh] = useState(0);
  // Ages are read when the page opens.
  const [now] = useState(() => Date.now());
  const projectWatch = useProjectResource();
  const project = projectWatch?.snapshot ?? null;
  const live = project?.live && !project.live.isRemoving ? project.live : null;
  const previewCommit = useSessionResource()?.resource?.preview?.sourceCommit ?? "";
  const progress = usePublishProgress(
    projectId,
    focusOf(query),
    `${live?.revision ?? ""}:${project?.activeDeployment?.deploymentId ?? ""}:${refresh}`,
  );
  const publishing = progress.outcome === "running" || progress.outcome === "cancelling";
  const history = useLiveHistory(projectId, `${live?.revision ?? ""}:${progress.outcome ?? ""}:${refresh}`);
  const build = progress.facts?.build?.buildRevisionId ?? "";
  const deployment = progress.facts?.deployment?.deploymentId ?? "";
  const actor =
    history.items.find(
      (event) => (build && event.buildRevisionId === build) || (deployment && event.deploymentId === deployment),
    )?.actorDisplay ?? "";
  const versions = useReleaseVersions(projectId, live?.releaseId ?? null, `${refresh}:${progress.outcome}`);
  const releases = usePagedList({
    key: projectId,
    fetchPage: async (cursor, signal) => {
      const page = await listReleases({ projectId, pageSize: 50, pageToken: cursor }, signal);
      return { items: page.releases, nextPageToken: page.nextPageToken };
    },
    idOf: (release) => release.releaseId,
    reloadKey: `${refresh}:${progress.outcome}`,
  });

  const releaseOf = (commit: string) => releases.items.find((item) => item.sourceCommit === commit) ?? null;
  const rows: VersionRow[] = [
    ...versions.candidates.map((candidate) => ({
      sourceCommit: candidate.sourceCommit,
      subject: candidate.subject,
      version: candidate.version,
      committedAt: timestampToISO(candidate.committedAt),
      release: releaseOf(candidate.sourceCommit),
    })),
    ...releases.items
      .filter((release) => !versions.candidates.some((candidate) => candidate.sourceCommit === release.sourceCommit))
      .map((release) => ({
        sourceCommit: release.sourceCommit,
        subject: release.preparedSource?.version?.subject ?? "",
        version: release.preparedSource?.version ?? null,
        committedAt: timestampToISO(release.createdAt),
        release,
      })),
  ];
  const requestedRelease = query.get("release") ?? "";
  const selectedCommit =
    query.get("commit") ||
    (RELEASE_ID_COMMIT.test(requestedRelease) ? requestedRelease : "") ||
    rows[0]?.sourceCommit ||
    "";
  const selected = rows.find((row) => row.sourceCommit === selectedCommit) ?? {
    sourceCommit: selectedCommit,
    subject: "",
    version: null,
    committedAt: null,
    release: null,
  };
  const liveIndex = live ? rows.findIndex((row) => row.sourceCommit === live.releaseId) : -1;
  const selectedIndex = rows.findIndex((row) => row.sourceCommit === selectedCommit);
  const position: VersionPosition =
    live?.releaseId === selectedCommit ? "live" : liveIndex >= 0 && selectedIndex > liveIndex ? "behind" : "ahead";
  const publishAgain = () =>
    openPublishPopover(
      progress.facts?.kind === "settings" ? { mode: "live" } : { mode: "version", commit: progress.releaseId },
    );
  // The panel's width, not the window's, decides the columns: the chat and the Tools list take their share first.
  return (
    <div className="@container min-h-0 flex-1 overflow-y-auto px-6.5 pt-5.5 pb-10">
      <div className="mx-auto max-w-[1400px] space-y-4">
        <header className="flex items-start gap-4">
          <h2 className="text-lg font-semibold text-ink">Publishing</h2>
          <Button className="ml-auto" size="sm" onClick={() => setRefresh((value) => value + 1)}>
            Refresh
          </Button>
        </header>
        {projectWatch?.error && <RequestProblem operation="Reading the Project" reason={projectWatch.error} />}
        {projectWatch?.disconnected && (
          <Notice>Project updates disconnected. Reconnecting to read current state…</Notice>
        )}
        {progress.error && <RequestProblem operation="Reading the publishing" reason={progress.error} />}
        <PublishProgress
          projectId={projectId}
          progress={progress}
          live={live}
          actor={actor}
          onPublishAgain={publishAgain}
        />
        <LiveDeployment projectId={projectId} project={project} publishing={publishing} />
        <div className="grid items-start gap-4 @4xl:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]">
          <section className="overflow-hidden rounded-[10px] border border-line-soft bg-panel" aria-label="Versions">
            <h3 className="px-3 pt-2.5 pb-2 text-[11px] font-normal tracking-[0.07em] text-ink-faint uppercase">
              Versions · newest first
            </h3>
            {versions.error && <RequestProblem operation="Reading the versions" reason={versions.error} />}
            {!versions.loaded && <p className="px-3 pb-3 text-sm text-ink-faint">Reading the versions…</p>}
            {versions.loaded && rows.length === 0 && (
              <p className="px-3 pb-3 text-sm text-ink-faint">
                No versions yet. The agent&apos;s first change makes one.
              </p>
            )}
            <ul aria-label="Versions" className="max-h-[70vh] overflow-y-auto">
              {rows.map((row) => (
                <li key={row.sourceCommit}>
                  <Link
                    href={queryHref((next) => {
                      next.set("commit", row.sourceCommit);
                      next.set("release", row.sourceCommit);
                      next.delete("build");
                      next.delete("deployment");
                    })}
                    aria-current={row.sourceCommit === selectedCommit ? "true" : undefined}
                    className={cx(
                      "flex w-full items-center gap-2.5 border-b border-line-faint px-3 py-2.25 text-[13px] focus-visible:shadow-[inset_0_0_0_2px_var(--color-accent-rule)] focus-visible:outline-none",
                      row.sourceCommit === selectedCommit ? "bg-accent-wash" : "hover:bg-hover",
                    )}
                  >
                    <span className="w-15 shrink-0 font-semibold text-ink" title={commitDetail(row.sourceCommit)}>
                      {versionNumber(row.version)}
                    </span>
                    <span className="w-19 shrink-0 text-xs text-ink-faint">{formatTimeAgo(row.committedAt, now)}</span>
                    <span className="min-w-0 flex-1 truncate text-ink">
                      {row.subject ||
                        (row.release
                          ? `Release · ${enumText(row.release.status, "RELEASE_STATUS").toLowerCase()}`
                          : "")}
                    </span>
                    {row.sourceCommit === previewCommit && <VersionTag>Preview</VersionTag>}
                    {row.sourceCommit === live?.releaseId && <VersionTag live>Live</VersionTag>}
                    {row.release?.readyBuildRevisionId && row.sourceCommit !== live?.releaseId && (
                      <span className="shrink-0 text-[10.5px] text-ink-faint">built</span>
                    )}
                  </Link>
                </li>
              ))}
            </ul>
            {releases.nextPageToken && (
              <Button className="m-3" size="sm" disabled={releases.loading} onClick={() => void releases.loadMore()}>
                Load older releases
              </Button>
            )}
          </section>
          {selectedCommit ? (
            <VersionDetail
              key={selectedCommit}
              projectId={projectId}
              version={selected}
              position={position}
              project={project}
              publishing={publishing}
              onRollback={(deploymentId) =>
                replaceQuery((next) => {
                  next.delete("build");
                  next.set("deployment", deploymentId);
                })
              }
            />
          ) : (
            <Notice>Select a version.</Notice>
          )}
        </div>
        <LiveHistory events={history} />
      </div>
    </div>
  );
}
