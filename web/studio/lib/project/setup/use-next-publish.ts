// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { configurationDraftQueue } from "@/lib/api/flow/draft/configuration-draft";
import { useProjectConfiguration } from "@/lib/project/use-project-configuration";
import { flowApiRoutes } from "@/lib/shared/studio-routes";
import { liveDifferenceCount, setupItems } from "./configuration-items";

/**
 * Live's next-publish layer, one per Project, as Setup edits it: checked against the Release of `commit`
 * (current Live's on the Live tab, a version being published) or, with none, the Preview's source, as the
 * Preview tab's Next publish column is. Each item follows Preview or is Live's own and is waiting when it
 * differs from what current Live runs. It is read again whenever the layer's autosave queue settles or
 * `version` changes, such as when Preview applies new settings, which followed items take; a null project
 * reads nothing.
 */
export function useNextPublish(projectId: string | null, commit: string | null, version = "") {
  const basePath = flowApiRoutes.liveNextPublish(projectId ?? "", commit);
  const queue = configurationDraftQueue(basePath);
  const draft = useSyncExternalStore(queue.subscribe, queue.getSnapshot, queue.getSnapshot);
  const read = useProjectConfiguration(
    projectId === null ? null : { projectId, scope: "PROJECT_SCOPE_LIVE", releaseId: commit ?? "", nextPublish: true },
    version,
  );
  const { configuration, retry } = read;
  const busy = draft.status === "saving" || draft.pending.length > 0;
  const wasBusy = useRef(busy);
  useEffect(() => {
    if (wasBusy.current && !busy) retry();
    wasBusy.current = busy;
  }, [busy, retry]);
  const items = useMemo(() => (configuration ? setupItems(configuration) : null), [configuration]);
  return {
    configuration,
    /** Every item by its autosave key, with the gaps that name it; null while it is read. */
    items,
    /** How many settings differ from what current Live runs, so they wait for a publish. */
    waiting: configuration ? liveDifferenceCount(configuration) : 0,
    error: read.error,
    loaded: read.loaded,
    basePath,
    /** Changes whenever the layer's revision does, so the editors read it again. */
    revision: configuration?.revision ?? "",
    refresh: retry,
  };
}

/**
 * Counts the times Setup's next-publish autosave settled, on the Preview tab's Next publish column or on the Live
 * tab (`liveCommit`'s), so a read of what waits for a publish outside Setup, such as the bar's Publish count,
 * follows Setup's edits at once.
 */
export function useNextPublishSettles(projectId: string | null, liveCommit: string | null): number {
  const column = configurationDraftQueue(flowApiRoutes.liveNextPublish(projectId ?? "", null));
  const liveTab = configurationDraftQueue(flowApiRoutes.liveNextPublish(projectId ?? "", liveCommit));
  const columnDraft = useSyncExternalStore(column.subscribe, column.getSnapshot, column.getSnapshot);
  const liveDraft = useSyncExternalStore(liveTab.subscribe, liveTab.getSnapshot, liveTab.getSnapshot);
  const busy = [columnDraft, liveDraft].some((draft) => draft.status === "saving" || draft.pending.length > 0);
  const [settles, setSettles] = useState(0);
  const wasBusy = useRef(busy);
  useEffect(() => {
    if (wasBusy.current && !busy) setSettles((count) => count + 1);
    wasBusy.current = busy;
  }, [busy]);
  return settles;
}
