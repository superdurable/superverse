// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { enumText } from "@/lib/shared/protobuf";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { probePublicUrl } from "@/lib/api/transport/public-url-probe";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import { PhaseTimings } from "@/components/ui/timings/PhaseTimings";
import { PREVIEW_PHASE_LABELS, previewStatusOf } from "@/lib/project/setup/environment-status";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";
import { cx } from "@/components/ui/cx";
import { statusDotClass } from "@/components/frame/status/EnvironmentStatusTab";
import { FoldedTimeline } from "@/components/project/activity/FoldedTimeline";
import { PreviewDeploymentStatus } from "./PreviewAutoDeploy";
import { usePreviewCommands } from "./use-preview-commands";

const BAR_BUTTON =
  "grid size-6.5 place-items-center rounded-md text-ink-faint hover:bg-hover hover:text-ink focus-visible:shadow-(--focus-ring) focus-visible:outline-none";

/** Opens the Preview in a new window, only once this browser can reach its public URL. */
function PreviewLink({ url }: { url: string }) {
  const [reachableUrl, setReachableUrl] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    void probePublicUrl(url, controller.signal).then(
      () => setReachableUrl(url),
      () => undefined,
    );
    return () => controller.abort();
  }, [url]);

  if (reachableUrl !== url)
    return <span className="px-1 text-[11px] whitespace-nowrap text-ink-faint">Verifying…</span>;
  return (
    <a
      aria-label="Open Preview"
      title="Open in a new window"
      className={BAR_BUTTON}
      href={url}
      rel="noreferrer"
      target="_blank"
    >
      <Glyph name="external" className="size-3.5" />
    </a>
  );
}

/**
 * The Interface on Preview: the application as its users see it, framed under an address bar with
 * the environment and its status, the address, reload and open in a new window, and Start or Stop.
 * When the Preview is not running it says why and offers Start Preview; its problem shows with its
 * fix. Under the application: the deployed commit, the way to its configuration, where it stands
 * beside the latest push with its automatic deployment, the phases of its start, and its history.
 * The parent remounts the panel for a new source or Sandbox instance, which starts new operations.
 */
export function PreviewPanel({ projectId }: { projectId: string }) {
  const {
    session,
    preview,
    status,
    runningDesired,
    stopping,
    retryCleanup,
    sourceReady,
    configurationReady,
    configurationError,
    allowed,
    busy,
    act,
    problem,
    remedyActions,
    configuration,
  } = usePreviewCommands(projectId);
  const [reloads, setReloads] = useState(0);
  // The newest start failed or was refused, as the bar's Preview status reports it.
  const failed = previewStatusOf(preview, null).kind === "FAILED";
  const shown = previewStatusOf(preview, configuration);
  const ready = preview?.status === "PREVIEW_STATUS_READY" && Boolean(preview.url);
  const statusWords = session.loading
    ? "loading"
    : status
      ? enumText(status, "PREVIEW_STATUS").replaceAll("_", " ").toLowerCase()
      : "not started";
  const control =
    runningDesired || stopping ? (
      <Button size="sm" disabled={!allowed.stop} onClick={() => void act("stop")}>
        {busy === "stop" ? "Stopping…" : retryCleanup ? "Retry Preview cleanup" : stopping ? "Stopping…" : "Stop"}
      </Button>
    ) : (
      <Button variant="primary" size="sm" disabled={!allowed.start} onClick={() => void act("start")}>
        {busy === "start" ? "Starting…" : "Start Preview"}
      </Button>
    );
  return (
    <section className="flex flex-col gap-2" aria-label="Project Preview">
      <div className="flex items-center gap-1.5 rounded-[9px] border border-line-soft bg-panel py-1 pr-1.5 pl-2">
        <span
          className="inline-flex h-4.5 shrink-0 items-center gap-1 rounded-full border border-line-soft px-1.75 text-[10.5px] font-medium text-ink-dim"
          title={`Preview: ${shown.detail}`}
        >
          <span
            aria-hidden="true"
            className={cx("size-1.5 rounded-full border", statusDotClass("preview", shown.tone))}
          />
          Preview
        </span>
        <span
          className="min-w-0 flex-1 truncate font-mono text-xs text-ink-dim"
          title={preview?.url || undefined}
          data-testid="preview-address"
        >
          {ready ? preview?.url : statusWords}
        </span>
        {ready && preview?.url && (
          <>
            <button
              type="button"
              aria-label="Reload the Preview"
              title="Reload"
              className={BAR_BUTTON}
              onClick={() => setReloads((value) => value + 1)}
            >
              <Glyph name="refresh" className="size-3.5" />
            </button>
            <PreviewLink url={preview.url} />
          </>
        )}
        {control}
      </div>
      {problem && <ProblemPanel problem={problem} actions={remedyActions} />}
      {preview?.status === "PREVIEW_STATUS_READY" && preview.url ? (
        <iframe
          key={reloads}
          title="Preview application"
          src={preview.url}
          className="h-[calc(100dvh-13rem)] min-h-96 w-full rounded-[10px] bg-white shadow-(--shadow-lift)"
        />
      ) : (
        <div className="mt-6 flex flex-col items-center gap-1.5 rounded-xl border border-line-soft bg-panel px-6 py-7 text-center">
          <b className="text-[15px] text-ink">
            {status === "PREVIEW_STATUS_STARTING"
              ? "Preview is waking up"
              : failed
                ? "Preview did not start"
                : "Preview is not running"}
          </b>
          <p className="max-w-sm text-[13.5px]/normal text-ink-faint">
            {status === "PREVIEW_STATUS_STARTING"
              ? "The app appears here when it is ready, usually within a minute."
              : failed
                ? "Fix the cause shown above in the application or its configuration, then start Preview again."
                : !sourceReady
                  ? "Its source is not prepared. The agent's next push deploys it, or prepare the current source and start it."
                  : !configurationReady
                    ? configurationError || "Validate the Preview configuration before starting it."
                    : "Start it to see the app as its users do."}
          </p>
          {!runningDesired && (failed || !sourceReady || !configurationReady) && (
            <Link
              className="text-[13px] text-accent hover:underline"
              href={studioRoutes.previewConfiguration(projectId)}
            >
              Open Configuration
            </Link>
          )}
        </div>
      )}
      <div className="rounded-[10px] border border-line-soft bg-panel px-3.5 py-3 text-xs">
        <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
          {preview?.sourceCommit && (
            <p className="min-w-0 font-mono text-[11px] break-all text-ink-faint">
              Deployed commit: {preview.sourceCommit}
            </p>
          )}
          <Link
            className="ml-auto text-[11px] text-accent hover:underline"
            href={studioRoutes.previewConfiguration(projectId)}
          >
            Configure Preview
          </Link>
        </div>
        <div className="mt-2">
          <PreviewDeploymentStatus projectId={projectId} />
        </div>
        {preview && preview.phaseTimings.length > 0 && (
          <div className="mt-3 border-t border-line-soft pt-3">
            <PhaseTimings title="Preview start phases" timings={preview.phaseTimings} labels={PREVIEW_PHASE_LABELS} />
          </div>
        )}
        <FoldedTimeline className="mt-3 border-t border-line-soft pt-3" projectId={projectId} scope="preview" />
      </div>
    </section>
  );
}
