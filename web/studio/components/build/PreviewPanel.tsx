// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";
import { FailureRemedy } from "@/lib/generated/protobuf/superverse/v2/FailureRemedy";
import { enumText } from "@/lib/shared/protobuf";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { probePublicUrl } from "@/lib/api/transport/public-url-probe";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import { PhaseTimings } from "@/components/ui/timings/PhaseTimings";
import { PREVIEW_PHASE_LABELS, previewStatusOf, startDurationWords } from "@/lib/project/setup/environment-status";
import { commitDetail, versionNumber } from "@/lib/shared/version/version";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";
import { statusDotClass } from "@/components/frame/status/EnvironmentStatusTab";
import { EnvironmentTimeline } from "@/components/project/activity/EnvironmentTimeline";
import { BAR_BUTTON, InterfaceBar, InterfaceDetails } from "./InterfaceBar";
import { PreviewDeploymentStatus } from "./PreviewAutoDeploy";
import { usePreviewCommands } from "./use-preview-commands";

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

/** The middle of the Interface while no page shows: what is happening, and the next step. */
function Empty({ heading, children }: { heading: string; children: ReactNode }) {
  return (
    <div className="mx-auto my-5 flex max-w-sm flex-col items-center gap-1.5 text-center">
      <b className="text-[14px] text-ink">{heading}</b>
      {children}
    </div>
  );
}

/**
 * The Interface on Preview, as the prototype draws it: an address bar with "Preview V12" and its status
 * dot, the address, reload, open in a new window and Stop while it runs; then the app's page. While it
 * starts, its phases; failed, its problem with Start Preview; missing settings, one way to Setup (the
 * problem's own Open Setup when it has one); stopped, Start Preview. Under it, folded under Details: the deployed commit, where it stands beside the latest
 * push, its last start's phases and its history. The parent remounts the panel for a new source or
 * Sandbox instance, which starts new operations.
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
  const version = versionNumber(preview?.servingVersion ?? preview?.preparedSource?.version ?? null, "");
  const statusWords = session.loading
    ? "loading"
    : status
      ? enumText(status, "PREVIEW_STATUS").replaceAll("_", " ").toLowerCase()
      : "not started";
  const start = (
    <Button variant="primary" size="sm" className="mt-2" disabled={!allowed.start} onClick={() => void act("start")}>
      <Glyph name="play" className="size-3" />
      {busy === "start" ? "Starting…" : "Start Preview"}
    </Button>
  );
  // One way to Setup: when the problem above leads there, the middle adds no second link.
  const problemOpensSetup = problem?.remedy === FailureRemedy.FAILURE_REMEDY_OPEN_SETUP;
  const setupLink = problemOpensSetup ? null : (
    <Link className="text-[12.5px] text-accent hover:underline" href={studioRoutes.setup(projectId, "preview")}>
      Open Setup
    </Link>
  );
  let body: ReactNode;
  if (ready && preview?.url)
    body = (
      <iframe
        key={reloads}
        title="Preview application"
        src={preview.url}
        className="h-[calc(100dvh-13rem)] min-h-96 w-full rounded-[10px] bg-white shadow-(--shadow-lift)"
      />
    );
  else if (status === "PREVIEW_STATUS_STARTING")
    body = (
      <div className="rounded-[10px] border border-line-soft bg-panel px-4 py-3.5">
        <b className="text-[14px] text-ink">Preview is starting{version && ` ${version}`}</b>
        <p className="mt-1 mb-2.5 text-[12.5px] text-ink-faint">
          About {startDurationWords(preview).replace(/^about /, "")}. The page appears when it&apos;s ready.
        </p>
        {preview && preview.phaseTimings.length > 0 && (
          <PhaseTimings title="Preview start phases" timings={preview.phaseTimings} labels={PREVIEW_PHASE_LABELS} />
        )}
      </div>
    );
  else if (runningDesired || stopping)
    body = <Empty heading="Stopping Preview">The page comes back when it starts.</Empty>;
  else if (failed && sourceReady && configurationReady)
    body = (
      <Empty heading={`Preview did not start${version && ` ${version}`}`}>
        <p className="text-[12.5px] text-ink-faint">The problem above says why; start it again once it is fixed.</p>
        {start}
      </Empty>
    );
  else if (!sourceReady)
    body = (
      <Empty heading="The latest push isn't built yet">
        <p className="text-[12.5px] text-ink-faint">The agent&apos;s next push builds and starts it.</p>
        {setupLink}
      </Empty>
    );
  else if (!configurationReady)
    // The problem above already says what is missing and opens Setup.
    body = problemOpensSetup ? null : (
      <Empty heading="Preview needs setup before it starts">
        <p className="text-[12.5px] text-ink-faint">{configurationError || "Setup lists what is missing."}</p>
        {setupLink}
      </Empty>
    );
  else
    body = (
      <Empty heading="Preview is stopped">
        <p className="text-[12.5px] text-ink-faint">Start it to see the page. Live is not affected.</p>
        {start}
      </Empty>
    );
  return (
    <section className="flex flex-col gap-2" aria-label="Project Preview">
      <InterfaceBar
        environment="Preview"
        dot={statusDotClass("preview", shown.tone)}
        version={version}
        title={`Preview: ${shown.detail}${preview?.sourceCommit ? ` · ${commitDetail(preview.sourceCommit)}` : ""}`}
        address={ready && preview?.url ? preview.url : statusWords}
      >
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
        {(runningDesired || stopping) && (
          <Button size="sm" variant="quiet" disabled={!allowed.stop} onClick={() => void act("stop")}>
            <Glyph name="stop" className="size-3" />
            {busy === "stop" ? "Stopping…" : retryCleanup ? "Retry Preview cleanup" : stopping ? "Stopping…" : "Stop"}
          </Button>
        )}
      </InterfaceBar>
      {problem && <ProblemPanel problem={problem} actions={remedyActions} />}
      {body}
      <InterfaceDetails>
        {() => (
          <>
            {preview?.sourceCommit && (
              <p className="font-mono text-[11px] break-all text-ink-faint">Deployed commit: {preview.sourceCommit}</p>
            )}
            <PreviewDeploymentStatus projectId={projectId} />
            {preview && preview.phaseTimings.length > 0 && status !== "PREVIEW_STATUS_STARTING" && (
              <PhaseTimings title="Preview start phases" timings={preview.phaseTimings} labels={PREVIEW_PHASE_LABELS} />
            )}
            <EnvironmentTimeline projectId={projectId} scope="preview" />
          </>
        )}
      </InterfaceDetails>
    </section>
  );
}
