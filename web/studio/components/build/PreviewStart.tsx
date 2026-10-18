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
import { startDurationWords } from "@/lib/project/setup/environment-status";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Button, buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { PopoverCard, usePopover } from "@/components/ui/Popover";
import { usePreviewCommands } from "./use-preview-commands";

/**
 * A stopped Preview's word in the bar, "· stopped", which opens Start Preview from any tab: nothing else
 * starts a stopped Preview but the agent's next push. Start takes Setup's saved settings; it waits while
 * the latest push is not built, Setup misses something or a push's update runs, and says which.
 */
export function PreviewStartWord({
  projectId,
  word,
  selected,
}: {
  projectId: string;
  /** The status's word, "stopped" or "not started". */
  word: string;
  /** The Preview tab is selected, so the word reads on its fill. */
  selected: boolean;
}) {
  const anchor = useRef<HTMLSpanElement>(null);
  const popover = usePopover(anchor);
  const { preview, allowed, busy, act, sourceReady, configurationReady, pushUpdating, session } =
    usePreviewCommands(projectId);
  const never = !preview || preview.status === "PREVIEW_STATUS_NOT_STARTED";
  const waitingFor =
    busy === "start"
      ? null
      : pushUpdating
        ? "Updating to the latest push…"
        : session.status !== "SESSION_STATUS_ACTIVE"
          ? "The project's workspace is not ready yet."
          : !sourceReady
            ? "The latest push is not built yet."
            : !configurationReady
              ? "Setup misses something first."
              : null;
  return (
    <span ref={anchor} className="inline-flex min-w-0">
      <button
        type="button"
        aria-haspopup="dialog"
        aria-expanded={popover.open}
        aria-label={`Preview ${word}: start it`}
        title="Start Preview"
        onClick={popover.toggle}
        className={cx(
          "-ml-1 max-w-36 min-w-0 truncate rounded-[6px] pr-2.25 font-medium group-data-[tight=true]/bar:max-w-20 hover:underline",
          "focus-visible:shadow-(--focus-ring) focus-visible:outline-none",
          selected ? "text-panel" : "text-ink-faint",
        )}
        data-testid="environment-preview-status"
        data-tone="plain"
      >
        · {busy === "start" ? "starting" : word}
      </button>
      {popover.open && (
        <PopoverCard anchorRef={anchor} label="Start Preview" className="w-[300px] max-w-[calc(100vw-16px)]">
          <div className="grid gap-2 p-1.5 text-[12.5px]" data-testid="preview-start">
            <p className="font-semibold text-ink">{never ? "Preview hasn't started" : "Preview is stopped"}</p>
            <p className="text-ink-dim">
              It starts with the latest version and Setup&apos;s settings, in {startDurationWords(preview)}. The
              agent&apos;s next push also starts it.
            </p>
            {waitingFor && (
              <p className="text-ink-faint">
                {waitingFor}{" "}
                {!configurationReady && sourceReady && !pushUpdating && (
                  <Link
                    className="text-accent hover:underline"
                    href={studioRoutes.setup(projectId, "preview")}
                    onClick={popover.close}
                  >
                    Open Setup
                  </Link>
                )}
              </p>
            )}
            <div className="flex justify-end">
              <Button
                variant="primary"
                size="sm"
                disabled={!allowed.start}
                onClick={() => {
                  popover.close();
                  void act("start");
                }}
              >
                {busy === "start" ? "Starting…" : "Start Preview"}
              </Button>
            </div>
          </div>
        </PopoverCard>
      )}
    </span>
  );
}

/**
 * Start Preview where a stopped Preview leaves a surface empty, such as Runs and Inbox. A Preview that lacks
 * settings cannot start, so there it leads to Setup instead of offering a Start that stays disabled.
 */
export function PreviewStartButton({ projectId }: { projectId: string }) {
  const { allowed, busy, act, sourceReady, configurationReady } = usePreviewCommands(projectId);
  if (sourceReady && !configurationReady)
    return (
      <span className="mt-4 flex flex-col items-center gap-2">
        It needs setup before it starts.
        <Link href={studioRoutes.setup(projectId, "preview")} className={buttonStyle("primary", "sm")}>
          Open Setup
        </Link>
      </span>
    );
  return (
    <Button variant="primary" size="sm" className="mt-4" disabled={!allowed.start} onClick={() => void act("start")}>
      {busy === "start" ? "Starting…" : "Start Preview"}
    </Button>
  );
}
