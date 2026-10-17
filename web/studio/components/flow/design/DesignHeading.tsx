// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { Badge, type BadgeTone } from "@/components/ui/badge/Badge";

/**
 * The process's title and one small status chip on the top-left corner of the canvas while its design is
 * drafted or built, such as "Drafting" or "Building · 4 of 6 steps written". It covers only the band the
 * canvas keeps clear above the drawing, never a Step, and takes no pointer, so the drawing stays reachable.
 */
export function DesignHeading({ title, status, tone = "accent" }: { title: string; status: string; tone?: BadgeTone }) {
  return (
    <div className="pointer-events-none absolute top-2.5 left-3 z-5 flex max-w-[calc(100%-1.5rem)] min-w-0 items-center gap-2">
      <h2 className="min-w-0 truncate rounded-md bg-bg/88 px-1.5 py-0.5 text-title font-semibold text-ink backdrop-blur-[6px]">
        {title}
      </h2>
      <Badge tone={tone} className="shrink-0" data-testid="design-status">
        {status}
      </Badge>
    </div>
  );
}
