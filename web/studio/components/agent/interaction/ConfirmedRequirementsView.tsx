// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ConfirmedRequirements } from "@/lib/session/agent-types";
import { RequirementsSpec } from "./RequirementsSpec";

/** The current version of what the agent builds, one disclosure the reader can open at any time. */
export function ConfirmedRequirementsView({ confirmed }: { confirmed: ConfirmedRequirements }) {
  return (
    <details className="rounded-lg border border-line-soft bg-panel px-3 py-2 text-[12px] text-ink-dim">
      <summary className="cursor-pointer font-medium">
        What the agent builds · version {String(confirmed.revision)}: {confirmed.spec.title}
      </summary>
      <div className="mt-2">
        <RequirementsSpec spec={confirmed.spec} />
      </div>
    </details>
  );
}
