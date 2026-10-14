// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import { NotReady } from "@/components/ui/NotReady";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowRouteParams } from "@/lib/flow/FlowNavigationProvider";
import { LazyDiagnosticsView, LazyInboxView, LazyRunsView } from "./LazyFlowViews";

/** The Flow root, for a Studio surface that hosts Flow views beside its own controls (Build). */
export { FlowFrame } from "@/components/flow/frame/FlowFrame";

/**
 * Runs of the catalog's Flow Types, or one run's diagnostics with ?diagnostics=true. The caller's
 * `events`, what the environment's triggers received, shows beside the runs as Runs | Events.
 */
export function RunsSurface({ events }: { events?: ReactNode } = {}) {
  const { flowType, flowId, runId, diagnostics } = useFlowRouteParams();
  if (diagnostics) return <LazyDiagnosticsView key={`${flowId}:${runId}`} />;
  return (
    <FlowDefinitionGate surfaceName="Runs">
      <LazyRunsView key={flowType} events={events} />
    </FlowDefinitionGate>
  );
}

/** Human work waiting in the catalog's Flows. */
export function InboxSurface() {
  const { flowType } = useFlowRouteParams();
  return (
    <FlowDefinitionGate surfaceName="Inbox">
      <LazyInboxView key={flowType} />
    </FlowDefinitionGate>
  );
}

/**
 * Renders its view only for a catalog of exactly the App's valid Flow
 * Definitions, and only for a requested Flow Type that is one of them.
 */
function FlowDefinitionGate({ children, surfaceName }: { children: ReactNode; surfaceName: string }) {
  const { flowsAvailable, catalog, definitions, error, ready } = useFlowCatalog();
  const { flowType: requestedFlowType } = useFlowRouteParams();
  if (!ready)
    return (
      <NotReady kind="placeholder" heading="Loading Flow Definitions">
        The current App catalog is still loading.
      </NotReady>
    );
  if (error)
    return (
      <NotReady kind="error" heading={`${surfaceName} did not load`} retry>
        {error} Nothing has stopped; this is only the view.
      </NotReady>
    );
  const validFlowTypes = new Set(
    (definitions?.definitions ?? []).filter((definition) => definition.valid).map((definition) => definition.flowName),
  );
  const catalogFlowTypes = catalog?.flows.map((entry) => entry.flowType) ?? [];
  const exactlyValid =
    catalogFlowTypes.length > 0 &&
    new Set(catalogFlowTypes).size === catalogFlowTypes.length &&
    catalogFlowTypes.every((flowType) => validFlowTypes.has(flowType));
  if (!flowsAvailable || !exactlyValid)
    return (
      <NotReady kind="unavailable" heading="No processes in this version yet">
        {surfaceName} shows the runs of this version&apos;s processes once it has valid ones. Describe what the project
        should do in the chat, and it will not display a sample or a Flow from another App.
      </NotReady>
    );
  if (requestedFlowType && !validFlowTypes.has(requestedFlowType)) {
    return (
      <NotReady kind="unavailable" heading="Selected Flow Type is unavailable">
        {surfaceName} will not substitute another Flow Definition for {requestedFlowType}.
      </NotReady>
    );
  }
  return children;
}
