// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useRef } from "react";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowNavigation, useFlowRouteParams } from "@/lib/flow/FlowNavigationProvider";

/**
 * The Flow type and run selected in the URL for Runs or Inbox. Without a Flow type ("All
 * processes") the views list every Flow type's runs, and a selected run names only its Flow ID.
 * RunsSurface and InboxSurface mount these views only after verifying a non-empty runtime catalog.
 */
export function useFlowTypeRoute(surface: "runs" | "inbox") {
  const { flowType, flowId } = useFlowRouteParams();
  const { navigate } = useFlowNavigation();
  const { catalog, definitionUpdateKey } = useFlowCatalog();

  // A definition that changed while this view was open invalidates the selected run, so only the
  // Flow type is kept. Runs selected afterwards belong to the new definition and stay selected.
  const handledUpdateKey = useRef(definitionUpdateKey);
  useEffect(() => {
    if (definitionUpdateKey === handledUpdateKey.current) return;
    handledUpdateKey.current = definitionUpdateKey;
    navigate({ surface, flowType: flowType || undefined }, { replace: true });
  }, [definitionUpdateKey, flowType, navigate, surface]);

  /** Selects a run, keeping the Flow type of the URL; all processes stay all processes. */
  const open = useCallback(
    (nextFlowId?: string) => navigate({ surface, flowType: flowType || undefined, flowId: nextFlowId }),
    [flowType, navigate, surface],
  );

  if (!catalog) throw new Error(`${surface} requires a loaded Flow catalog`);
  const entry = catalog.flows.find((candidate) => candidate.flowType === flowType);
  return { flowType, flowId, entry, catalog, open, allProcesses: flowType === "" };
}
