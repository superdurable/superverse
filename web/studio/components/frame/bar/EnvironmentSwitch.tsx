// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import type { StatusFix } from "@/lib/project/setup/environment-status";
import type { ProjectEnvironment } from "@/lib/shared/studio-routes";
import { EnvironmentStatusTab, type TabStatus } from "@/components/frame/status/EnvironmentStatusTab";

/**
 * Preview | Live: which environment every surface shows, each with one status dot. A word follows an
 * environment's name only when something is off, and it leads to the fix (`fixHrefOf`, which those
 * who cannot fix it go without). Without a Live deployment the Live name is a label that says so; a
 * role that works on Live only sees Live alone. A pending Setup write adds "Saving…" after them.
 */
export function EnvironmentSwitch({
  environment,
  hasLive,
  liveOnly = false,
  hrefOf,
  fixHrefOf,
  previewStatus,
  liveStatus,
  saving,
}: {
  environment: ProjectEnvironment;
  hasLive: boolean;
  /** The reader works on Live only: Preview is absent. */
  liveOnly?: boolean;
  hrefOf: (environment: ProjectEnvironment) => string;
  /** Where a status's word leads, for those who may act on it. */
  fixHrefOf?: (environment: ProjectEnvironment, fix: StatusFix) => string;
  previewStatus?: TabStatus;
  liveStatus?: TabStatus;
  /** What the bar says of unsaved Setup writes, such as "Saving…". */
  saving?: ReactNode;
}) {
  const fixHref = (environment: ProjectEnvironment, status: TabStatus | undefined) =>
    status?.word && status.fix && fixHrefOf ? fixHrefOf(environment, status.fix) : null;
  return (
    <div className="flex min-w-0 items-center gap-2">
      <div
        role="group"
        aria-label="Which environment you are looking at"
        className="inline-flex min-w-0 shrink gap-0.5 rounded-[9px] bg-well p-0.75"
      >
        {!liveOnly && (
          <EnvironmentStatusTab
            environment="preview"
            name="Preview"
            selected={environment === "preview"}
            href={hrefOf("preview")}
            status={previewStatus ?? null}
            fixHref={fixHref("preview", previewStatus)}
            testId="environment-preview"
          />
        )}
        <EnvironmentStatusTab
          environment="live"
          name="Live"
          selected={environment === "live" || (liveOnly && !hasLive)}
          href={hasLive ? hrefOf("live") : null}
          status={liveStatus ?? null}
          title={hasLive ? undefined : "Live: not published yet. Live appears with the first publish."}
          fixHref={fixHref("live", liveStatus)}
          testId="environment-live"
        />
      </div>
      {saving}
    </div>
  );
}
