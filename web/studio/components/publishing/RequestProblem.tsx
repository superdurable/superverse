// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import { errorProblem } from "@/lib/shared/problem/problem";
import { ProblemPanel, type RemedyActions } from "@/components/ui/problem/ProblemPanel";

/**
 * A failed request of the page as a problem: what the page tried, the server's reason with its
 * remedy, and the details. reason is the error, or the message of a failed read. Without
 * `announce`, it shows without an alert, as where another copy of the same problem announces it.
 */
export function RequestProblem({
  operation,
  reason,
  actions,
  announce = true,
}: {
  operation: string;
  reason: unknown;
  actions?: RemedyActions;
  announce?: boolean;
}) {
  return <ProblemPanel problem={errorProblem(reason, { operation })} actions={actions} announce={announce} />;
}

/** A neutral status line, in the same band as the Flow views' notices. */
export function Notice({ children }: { children: ReactNode }) {
  return (
    <p role="status" className="rounded-lg border border-line-soft bg-well px-3 py-2.5 text-[13px] text-ink-dim">
      {children}
    </p>
  );
}
