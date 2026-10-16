// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, type ReactNode } from "react";
import { usePreviewDraft } from "@/lib/project/setup/PreviewDraftProvider";
import type { EventEnvironment } from "./trigger-event-fates";
import { useRecentTriggerEvents, type RecentTriggerEvents } from "./use-recent-trigger-events";

const SCOPES = {
  Preview: "PROJECT_SCOPE_PREVIEW",
  Live: "PROJECT_SCOPE_LIVE",
} as const satisfies Record<EventEnvironment, string>;

/** One environment's recent trigger events, which the run list's problem line and log share. */
export interface RecentEventsRead extends RecentTriggerEvents {
  environment: EventEnvironment;
  /** Whether the run list names events at all: a process relays a trigger, or events arrived. */
  shown: boolean;
}

const RecentEventsContext = createContext<RecentEventsRead | null>(null);

/**
 * Reads one environment's recent trigger events once for everything below it. Without a trigger
 * that the platform relays the events are not read at all; where nothing cheap says, they are read
 * and named only once some arrived.
 */
export function RecentEventsProvider({
  projectId,
  environment,
  children,
}: {
  projectId: string;
  environment: EventEnvironment;
  children: ReactNode;
}) {
  const relayed = useRelayedTrigger(environment);
  const read = useRecentTriggerEvents(projectId, SCOPES[environment], relayed !== false);
  const value: RecentEventsRead = { ...read, environment, shown: relayed === true || read.events.length > 0 };
  return <RecentEventsContext.Provider value={value}>{children}</RecentEventsContext.Provider>;
}

export function useRecentEvents(): RecentEventsRead | null {
  return useContext(RecentEventsContext);
}

/**
 * Whether a process has a trigger the platform relays, from the trigger bindings of the Preview's
 * Setup, which the frame already reads; null where it is not at hand, as on Live or for an operator.
 */
function useRelayedTrigger(environment: EventEnvironment): boolean | null {
  const configuration = usePreviewDraft()?.configuration;
  if (environment !== "Preview" || !configuration) return null;
  return (
    configuration.items.some((item) => item.key?.kind === "KIND_TRIGGER_BINDING") ||
    configuration.requirementGaps.some(
      (gap) => gap.kind === "KIND_TRIGGER_BINDING_REQUIRED" || gap.kind === "KIND_TRIGGER_BINDING_INVALID",
    )
  );
}
