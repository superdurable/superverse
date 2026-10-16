// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { TodoItem } from "@/lib/session/agent-types";

/** The Agent's own todo list as one read-only line: how many items are done and the one it works on now. */
export function TodoProgress({ items }: { items: readonly TodoItem[] }) {
  if (items.length === 0) return null;
  const completedCount = items.filter((item) => item.status === "completed").length;
  const current = items.find((item) => item.status === "in_progress");
  return (
    <p className="text-caption text-ink-faint">
      <span className="font-medium text-ink-dim">
        {completedCount} of {items.length} done
      </span>
      {current !== undefined && ` · Now: ${current.content}`}
    </p>
  );
}
