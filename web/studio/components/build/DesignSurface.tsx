// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useProcessDefinitions } from "@/lib/project/ProcessDefinitionsProvider";
import { useSessionResource } from "@/lib/session/SessionProvider";
import type { ShownDesign } from "@/lib/session/design/use-flow-design";
import { designPhaseLabel } from "@/lib/shared/design-phase";
import { processName } from "@/lib/shared/format";
import { LazyDesignFlowView } from "@/components/flow/scope/LazyFlowViews";
import { NotReady } from "@/components/ui/NotReady";

/**
 * A designed Flow on the Flow tab, in place of the implemented one it changes. Its Steps are compared with
 * the current source only when that source is the Session head's.
 */
export function DesignSurface({ design, flowType }: { design: ShownDesign; flowType: string }) {
  const { currentSource } = useProcessDefinitions();
  const head = useSessionResource()?.resource?.headCommit;
  if (!design.body)
    return design.error ? (
      <NotReady kind="error" heading="The Flow design did not load" retry>
        {design.error}
      </NotReady>
    ) : (
      <NotReady kind="placeholder" heading="Reading the Flow design">
        The agent&apos;s design of {processName(flowType)} is loading.
      </NotReady>
    );
  return (
    <LazyDesignFlowView
      flowType={flowType}
      design={design.body}
      phase={design.phase}
      current={currentSource && currentSource.commit === head ? currentSource.graphs : null}
      features={design.features}
    />
  );
}

/** Above all processes while a design is shown: its label, its summary and a link to each designed Flow. */
export function DesignBar({ design, hrefOf }: { design: ShownDesign; hrefOf: (flowType: string) => string }) {
  return (
    <div
      role="status"
      aria-label="Flow design"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b border-line-soft bg-panel px-3 py-2 text-[12.5px]"
    >
      <strong className="font-semibold text-accent">{designPhaseLabel(design.phase)}</strong>
      <span className="text-ink-dim">{design.error ?? design.summary}</span>
      {design.body?.flows.map(({ flowType }) => (
        <Link key={flowType} href={hrefOf(flowType)} className="text-accent hover:underline">
          {processName(flowType)}
        </Link>
      ))}
      {design.error && (
        <button type="button" className="text-accent hover:underline" onClick={design.retry}>
          Try again
        </button>
      )}
    </div>
  );
}
