// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { EnvironmentTimeline, type TimelineScope } from "./EnvironmentTimeline";

/**
 * One environment's timeline folded under "History", as the Interface shows it under the
 * environment: it reads the history only once opened, and again each time it opens.
 */
export function FoldedTimeline({
  projectId,
  scope,
  className,
}: {
  projectId: string;
  scope: TimelineScope;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <details className={className} onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary className="cursor-pointer text-[12.5px] text-ink-dim hover:text-ink">History</summary>
      {open && (
        <div className="mt-2">
          <EnvironmentTimeline projectId={projectId} scope={scope} />
        </div>
      )}
    </details>
  );
}
