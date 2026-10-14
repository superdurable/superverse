// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, useEffect, useRef, useSyncExternalStore, type ReactNode } from "react";
import {
  configurationDraftQueue,
  configurationDrafts,
  type ConfigurationDraftQueue,
  type DraftSnapshot,
} from "@/lib/api/flow/draft/configuration-draft";
import { flowApiRoutes } from "@/lib/shared/studio-routes";
import { useProjectConfiguration } from "@/lib/project/use-project-configuration";

/** The Preview's Setup draft: its configuration as the server checks it, and this tab's autosave queue. */
export interface PreviewDraft {
  /** The configuration of the Preview's prepared source, null while it is read or without a source. */
  configuration: ReturnType<typeof useProjectConfiguration>["configuration"];
  error: string;
  /** Reads the configuration again. */
  refresh: () => void;
  queue: ConfigurationDraftQueue;
  draft: DraftSnapshot;
  /** Every scope's Setup writes in this tab, Preview's and Live's, as one state. */
  all: DraftSnapshot;
}

const PreviewDraftContext = createContext<PreviewDraft | null>(null);

/**
 * Owns the Preview's Setup draft for the frame: one configuration read, which the bar's status, the
 * Apply header and the Preview menu share, read again whenever the autosave queue settles or the source
 * or applied configuration it is read for changes. While any Setup write waits, leaving the page asks first.
 */
export function PreviewDraftProvider({
  projectId,
  source,
  children,
}: {
  projectId: string;
  /** The Preview's prepared source and applied configuration, read again when either changes; null without a ready source. */
  source: string | null;
  children: ReactNode;
}) {
  const queue = configurationDraftQueue(flowApiRoutes.previewConfiguration(projectId));
  const draft = useSyncExternalStore(queue.subscribe, queue.getSnapshot, queue.getSnapshot);
  const read = useProjectConfiguration(
    source === null ? null : { projectId, scope: "PROJECT_SCOPE_PREVIEW" },
    source ?? "",
  );
  const all = useSyncExternalStore(
    configurationDrafts.subscribe,
    configurationDrafts.getSnapshot,
    configurationDrafts.getServerSnapshot,
  );
  const busy = draft.status === "saving" || draft.pending.length > 0;
  const anyBusy = all.status === "saving" || all.pending.length > 0;
  const wasBusy = useRef(busy);
  const { retry } = read;
  useEffect(() => {
    if (wasBusy.current && !busy) retry();
    wasBusy.current = busy;
  }, [busy, retry]);
  useEffect(() => {
    if (!anyBusy) return;
    const onLeave = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", onLeave);
    return () => window.removeEventListener("beforeunload", onLeave);
  }, [anyBusy]);
  const value: PreviewDraft = {
    configuration: read.configuration,
    error: read.error,
    refresh: retry,
    queue,
    draft,
    all,
  };
  return <PreviewDraftContext.Provider value={value}>{children}</PreviewDraftContext.Provider>;
}

/** The Preview's Setup draft, or null outside a builder's frame (an operator sees Live only). */
export function usePreviewDraft(): PreviewDraft | null {
  return useContext(PreviewDraftContext);
}

/**
 * What the bar says about unsaved Setup writes: saving, saved, not saved with a retry, or not saved
 * because an item changed elsewhere; nothing when idle.
 */
export function draftWords(
  draft: DraftSnapshot,
): "Saving…" | "Saved" | "Not saved: retry" | "Not saved: changed elsewhere" | null {
  if (draft.conflicts.length > 0) return "Not saved: changed elsewhere";
  switch (draft.status) {
    case "saving":
      return "Saving…";
    case "failed":
      return "Not saved: retry";
    case "saved":
      return draft.pending.length > 0 ? "Saving…" : "Saved";
    case "idle":
      return draft.pending.length > 0 ? "Saving…" : null;
  }
}
