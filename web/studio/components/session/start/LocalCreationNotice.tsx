// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { ProjectCreationPairing } from "@/components/project/creation/ProjectCreationPairing";
import { Button } from "@/components/ui/Button";
import type { useLocalCreation } from "./use-local-creation";

/** An uncertain creation send requires checking the real conversation before an explicit manual retry. */
export function LocalCreationNotice({ creation }: { creation: ReturnType<typeof useLocalCreation> }) {
  return (
    <>
      {creation.waitingForConnection && <ProjectCreationPairing projectId={creation.projectId} />}
      {creation.error && (
        <p role="alert" className="text-[12px] text-fail">
          {creation.error}
        </p>
      )}
      {creation.uncertain && (
        <section className="rounded-lg border border-line-soft bg-panel p-3 text-sm text-ink-dim">
          <p>Your first message may already have arrived. Check the conversation before sending it again.</p>
          <Button size="sm" onClick={creation.acknowledgeUncertain}>
            I checked the conversation; enable manual sending
          </Button>
        </section>
      )}
    </>
  );
}
