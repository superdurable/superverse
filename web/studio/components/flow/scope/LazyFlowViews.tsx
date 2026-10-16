// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import dynamic from "next/dynamic";

// Flow views load on demand and only in the browser: their stored preferences use browser APIs.
function loading(label: string) {
  function FlowViewLoading() {
    return <div className="flex min-h-0 flex-1 items-center justify-center text-[14px] text-ink-faint">{label}</div>;
  }
  return FlowViewLoading;
}

export const LazyFlowCatalogProvider = dynamic(
  () => import("@/lib/flow/FlowCatalogProvider").then((m) => m.FlowCatalogProvider),
  { ssr: false, loading: loading("Loading…") },
);
export const LazyFlowCanvas = dynamic(() => import("@/components/flow/canvas/FlowCanvas").then((m) => m.FlowCanvas), {
  ssr: false,
  loading: loading("Drawing the process…"),
});
export const LazyDesignFlowView = dynamic(
  () => import("@/components/flow/design/DesignFlowView").then((m) => m.DesignFlowView),
  { ssr: false, loading: loading("Drawing the design…") },
);
export const LazyProjectMap = dynamic(() => import("@/components/flow/map/ProjectMap").then((m) => m.ProjectMap), {
  ssr: false,
  loading: loading("Drawing the project…"),
});
export const LazyRunsView = dynamic(() => import("@/components/flow/run/RunsView").then((m) => m.RunsView), {
  ssr: false,
  loading: loading("Loading runs…"),
});
export const LazyInboxView = dynamic(() => import("@/components/flow/run/InboxView").then((m) => m.InboxView), {
  ssr: false,
  loading: loading("Loading the inbox…"),
});
export const LazyDiagnosticsView = dynamic(
  () => import("@/components/flow/diagnostics/DiagnosticsView").then((m) => m.DiagnosticsView),
  { ssr: false, loading: loading("Loading diagnostics…") },
);
export const LazySetupChecklist = dynamic(
  () => import("@/components/flow/connections/SetupChecklist").then((m) => m.SetupChecklist),
  { ssr: false, loading: () => <p role="status">Loading Setup…</p> },
);
export const LazyTeamSpaceConnectionEditor = dynamic(
  () =>
    import("@/components/flow/connector/team-space/TeamSpaceConnectionEditor").then((m) => m.TeamSpaceConnectionEditor),
  { ssr: false, loading: () => <p role="status">Loading the connector…</p> },
);
export const LazyLiveIdentities = dynamic(
  () => import("@/components/flow/connections/LiveIdentities").then((m) => m.LiveIdentities),
  { ssr: false, loading: () => <p role="status">Reading the connections…</p> },
);

/**
 * Fetches the views a new Project opens on at its first-run switch while its conversation is still the whole page,
 * so the switch, which comes mid-session, never waits on a download that a dropped connection could fail. A failed
 * fetch is tried again when the view renders.
 */
export function preloadSwitchViews(): void {
  void Promise.all([
    import("@/lib/flow/FlowCatalogProvider"),
    import("@/components/flow/map/ProjectMap"),
    import("@/components/flow/design/DesignFlowView"),
    import("@/components/flow/canvas/FlowCanvas"),
  ]).catch(() => undefined);
}
