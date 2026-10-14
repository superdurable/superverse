// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import type { PreviewStartRecord__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewStartRecord";
import type { ProjectLive__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectLive";
import { usePublishProgress, type PublishFocus } from "@/lib/project/publishing/use-publish-progress";
import { liveDifferenceCount } from "@/lib/project/setup/configuration-items";
import { useProjectConfiguration } from "@/lib/project/use-project-configuration";
import type { ReleaseVersions } from "@/lib/project/use-release-versions";
import type { ProjectEnvironment } from "@/lib/shared/studio-routes";
import { cx } from "@/components/ui/cx";
import { buttonStyle } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import { Spinner } from "@/components/ui/Icons";
import { usePopover } from "@/components/ui/Popover";
import { usePublishOpenings, type PublishOpening } from "./publish-opener";
import { PublishPopover } from "./PublishPopover";

/**
 * The bar's Publish: it opens the Publish popover. On the Preview tab it counts the versions ahead of
 * Live ("2 new") and is the one primary action when there are any; on the Live tab it counts the settings
 * waiting for a publish ("1 waiting") and opens on Live's own version. While a publish runs it reads
 * "Publishing…" and is disabled. Once a publish is admitted, Tools ▸ Publishing opens on it.
 */
export function PublishButton({
  projectId,
  environment,
  versions,
  live,
  activeDeploymentId,
  previewSource,
  starts,
  links,
}: {
  projectId: string;
  environment: ProjectEnvironment;
  versions: ReleaseVersions;
  live: ProjectLive__Output | null;
  /** The deployment the Project reports in progress; a new one reads the publishing again. */
  activeDeploymentId: string;
  previewSource: string;
  starts: readonly PreviewStartRecord__Output[];
  links: { publishing: string; setup: (anchor: string) => string; preview: string };
}) {
  const router = useRouter();
  const anchorRef = useRef<HTMLSpanElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const popover = usePopover(anchorRef);
  // Closing the popover with Escape gives the keyboard back to the button; a link that leaves the page,
  // such as "Set up ›", moves it to where it leads instead.
  const wasOpen = useRef(false);
  useEffect(() => {
    const closed = wasOpen.current && !popover.open;
    wasOpen.current = popover.open;
    if (!closed) return;
    const here = window.location.href;
    const timer = setTimeout(() => {
      if (document.activeElement === document.body && window.location.href === here) buttonRef.current?.focus();
    }, 150);
    return () => clearTimeout(timer);
  }, [popover.open]);
  const [opening, setOpening] = useState<PublishOpening>({ mode: "version" });
  // A publish admitted here is followed by its identity until it settles, so the button says Publishing…
  // at once, before the read model lists it; otherwise the newest publish is read again as Live moves.
  const [started, setStarted] = useState<PublishFocus | null>(null);
  const progress = usePublishProgress(projectId, started, `${live?.revision ?? ""}:${activeDeploymentId}`);
  const running = progress.outcome === "running" || progress.outcome === "cancelling";
  if (started && progress.outcome && !running) setStarted(null);
  usePublishOpenings((next) => {
    if (running) return;
    setOpening(next);
    popover.setOpen(true);
  });
  const onLive = environment === "live" && live !== null;
  const waitingRead = useProjectConfiguration(
    onLive ? { projectId, scope: "PROJECT_SCOPE_LIVE", releaseId: live.releaseId, nextPublish: true } : null,
    // A publish that went Live, or one that failed, changes what waits; read it again with the publishing.
    `${live?.revision ?? ""}:${progress.outcome ?? ""}`,
  );
  const waiting = onLive && waitingRead.configuration ? liveDifferenceCount(waitingRead.configuration) : 0;
  const ahead = versions.ahead.length;
  const count = onLive ? waiting : ahead;
  const primary = !onLive && ahead > 0;
  const accepted = (commit: string, buildRevisionId: string) => {
    popover.close();
    setStarted({ releaseId: commit, buildRevisionId });
    const target = new URL(links.publishing, window.location.origin);
    target.searchParams.set("release", commit);
    target.searchParams.set("build", buildRevisionId);
    target.searchParams.delete("deployment");
    target.searchParams.delete("commit");
    router.push(`${target.pathname}${target.search}`, { scroll: false });
  };
  return (
    <span ref={anchorRef} className="inline-flex">
      <button
        ref={buttonRef}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={popover.open}
        aria-label={running ? "Publishing…" : count > 0 ? `Publish, ${count} ${onLive ? "waiting" : "new"}` : "Publish"}
        title={
          running
            ? "A publish is running; Tools ▸ Publishing shows it"
            : primary
              ? `${ahead} version${ahead === 1 ? "" : "s"} on Preview that Live doesn't have`
              : onLive
                ? "Publish Live's waiting settings, or a newer version"
                : "Live is up to date"
        }
        disabled={running}
        onClick={() => {
          setOpening(onLive ? { mode: "live" } : { mode: "version" });
          popover.toggle();
        }}
        className={cx(buttonStyle(primary ? "primary" : "secondary", "md"), "h-8 min-h-0 py-0")}
        data-testid="publish-to-live"
      >
        {running ? <Spinner className="size-3.5" /> : <Glyph name="upload" />}
        {/* A tight bar keeps only the icon; the name stays on the button. */}
        <span className="group-data-[tight=true]/bar:hidden">{running ? "Publishing…" : "Publish"}</span>
        {!running && count > 0 && (
          <span
            className={cx(
              "rounded-full px-1.5 text-[11px] font-semibold group-data-[tight=true]/bar:hidden",
              primary ? "bg-cta-on/20 text-cta-on" : "bg-well text-ink-dim",
            )}
            data-testid="publish-count"
          >
            {count} {onLive ? "waiting" : "new"}
          </span>
        )}
      </button>
      {popover.open && (
        <PublishPopover
          projectId={projectId}
          anchorRef={anchorRef}
          opening={opening}
          onOpening={setOpening}
          onClose={popover.close}
          onAccepted={accepted}
          versions={versions}
          live={live}
          previewSource={previewSource}
          starts={starts}
          links={links}
        />
      )}
    </span>
  );
}
