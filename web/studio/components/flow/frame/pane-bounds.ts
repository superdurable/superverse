// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { PaneBounds } from "@/components/ui/pane/PaneResizeHandle";
import { STORAGE_KEYS } from "@/lib/browser/storage";

/** The resizable Flow panes: where each keeps its width in CSS and storage, and how wide it may get. */

/** 250px, 200 to 380px open, folding to a 40px rail with the list's icon below 170px. */
export const LIST_PANE: PaneBounds = {
  cssVariable: "--v2-list-w",
  edge: "end",
  pane: "list",
  storageKey: STORAGE_KEYS.listPaneWidth,
  defaultWidth: 250,
  minWidth: 200,
  maxWidth: 380,
  maxFraction: 0.45,
  rail: { width: 40, snapBelow: 170 },
  dragFallback: (box) => box.width * 0.35,
};

/** The run's Step drawer and the Flow's Step panel open at 290px, as the prototype has them; 280px at least. */
export const DRAWER_PANE: PaneBounds = {
  cssVariable: "--v2-drawer-w",
  edge: "start",
  pane: "drawer",
  storageKey: STORAGE_KEYS.drawerPaneWidth,
  defaultWidth: 290,
  minWidth: 280,
  maxFraction: 0.5,
  dragFallback: () => 360,
};

export const STEP_PANEL_PANE: PaneBounds = {
  cssVariable: "--v2-panel-w",
  edge: "start",
  storageKey: STORAGE_KEYS.stepPanelWidth,
  defaultWidth: 290,
  minWidth: 280,
  maxFraction: 0.55,
  dragFallback: () => 360,
};

/** The Diagnostics sidebar that explains the selected event; it always stays readable. */
export const SELECTED_EVENT_PANE: PaneBounds = {
  cssVariable: "--run-sidebar-width",
  edge: "start",
  pane: "selected-event",
  storageKey: STORAGE_KEYS.selectedEventWidth,
  defaultWidth: 340,
  minWidth: 280,
  maxFraction: 0.5,
  dragFallback: () => 340,
  collapsible: false,
};
