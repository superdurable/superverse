// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useRef, useState } from "react";
import type { agentModelView } from "./agent-model";

type ModelChoice = NonNullable<ReturnType<typeof agentModelView>>;

/** Admission's response precedes the Watch: keep choices closed until its revision is visible. */
export function useModelChoice(
  revision: number,
  onSelect: (model: ModelChoice) => Promise<void>,
  refresh: () => Promise<void>,
) {
  const admitted = useRef<number | null>(null);
  const [pending, setPending] = useState<number | null>(null);
  const select = (model: ModelChoice) => {
    if (admitted.current !== null && revision < admitted.current) return;
    const next = revision + 1;
    admitted.current = next;
    setPending(next);
    void onSelect(model).then(
      () => {
        // A failed read retains the gate; the Watch or a later canonical read can release it.
        void refresh().catch(() => undefined);
      },
      () => {
        // The command owner reports and reconciles failures; never replay the mutation.
        if (admitted.current === next) {
          admitted.current = null;
          setPending(null);
        }
      },
    );
  };
  return { pending: pending !== null && revision < pending, select };
}
