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
import { commitDetail, versionLabel, versionNumber } from "@/lib/shared/version/version";
import { EnvironmentTimeline } from "@/components/project/activity/EnvironmentTimeline";
import { Glyph } from "@/components/ui/Glyph";
import { BAR_BUTTON, CopyAddress, InterfaceBar, InterfaceDetails } from "./InterfaceBar";

/**
 * The Interface on Live, as the prototype draws it: an address bar with "Live V12", the address, reload, open
 * in a new window and copy the Live link; then the app's page at its Live address, where what people do is
 * real. Under it, folded under Details: the version and commit Live runs, and Live's history.
 */
export function LiveInterface({ projectId, live }: { projectId: string; live: ProjectLive__Output }) {
  const [reloads, setReloads] = useState(0);
  return (
    <section className="flex flex-col gap-2" aria-label="Project Live">
      <InterfaceBar
        environment="Live"
        dot="border-pass bg-pass"
        version={versionNumber(live.version, "")}
        title={`Live runs ${versionLabel(live.version)} · ${commitDetail(live.releaseId)}`}
        address={live.url}
      >
        <button
          type="button"
          aria-label="Reload Live"
          title="Reload"
          className={BAR_BUTTON}
          onClick={() => setReloads((value) => value + 1)}
        >
          <Glyph name="refresh" className="size-3.5" />
        </button>
        <a
          aria-label="Open Live"
          title="Open in a new window"
          className={BAR_BUTTON}
          href={live.url}
          rel="noreferrer"
          target="_blank"
        >
          <Glyph name="external" className="size-3.5" />
        </a>
        <CopyAddress url={live.url} label="Live link" />
      </InterfaceBar>
      <iframe
        key={reloads}
        title="Live application"
        src={live.url}
        className="h-[calc(100dvh-13rem)] min-h-96 w-full rounded-[10px] bg-white shadow-(--shadow-lift)"
      />
      <InterfaceDetails>
        {() => (
          <>
            <p className="text-[12px] text-ink-dim">
              Live runs {versionLabel(live.version)}
              <span className="block font-mono text-[11px] break-all text-ink-faint">{live.releaseId}</span>
            </p>
            <EnvironmentTimeline projectId={projectId} scope="live" />
          </>
        )}
      </InterfaceDetails>
    </section>
  );
}
