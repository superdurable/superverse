// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ComponentProps } from "react";
import { FlowApiProvider, useFlowApiScope, type FlowApiScopeBinding } from "@/lib/flow/FlowApiProvider";
import type { SetupChecklist } from "@/components/flow/connections/SetupChecklist";

export type { SetupNotes } from "@/components/flow/connections/SetupChecklist";
import { LazySetupChecklist } from "./LazyFlowViews";

/** What the table takes besides its two configuration hosts. */
type TableProps = Omit<ComponentProps<typeof SetupChecklist>, "preview" | "next">;

/** Preview's host inside the next publish's, so the table holds both. */
function PreviewHost({ next, ...table }: TableProps & { next: FlowApiScopeBinding }) {
  return <LazySetupChecklist preview={useFlowApiScope()} next={next} {...table} />;
}

/** The next publish's host, read once it is mounted, then Preview's when the tab has a Preview column. */
function NextHost({ previewPath, ...table }: TableProps & { previewPath: string | null }) {
  const next = useFlowApiScope();
  if (!previewPath) return <LazySetupChecklist preview={null} next={next} {...table} />;
  return (
    <FlowApiProvider key={previewPath} basePath={previewPath}>
      <PreviewHost next={next} {...table} />
    </FlowApiProvider>
  );
}

/**
 * Tools, Setup's table over two writers: Preview's configuration host (`previewPath`, null on the Live tab)
 * and the next-publish layer (`nextPath`, the BFF's `/configuration/live/next` route), which the Next publish
 * cells write. Each host has its own autosave queue, keyed by its path; the bar's "Saving…" merges them.
 */
export function SetupChecklistSurface({
  previewPath,
  nextPath,
  ...table
}: TableProps & { previewPath: string | null; nextPath: string }) {
  return (
    <FlowApiProvider key={nextPath} basePath={nextPath}>
      <NextHost previewPath={previewPath} {...table} />
    </FlowApiProvider>
  );
}
