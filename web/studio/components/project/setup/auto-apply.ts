// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef } from "react";
import type { usePreviewCommands } from "@/components/build/use-preview-commands";

/** How long Setup waits after the last saved change before it updates the Preview, so a burst rebuilds once. */
const SETTLE_MS = 3000;

/**
 * Whether Setup should update the Preview with its saved settings, once nothing is missing: while it runs, when
 * they differ from what it runs; or when its automatic update to the latest push was refused for missing
 * settings, which these now complete. A stopped Preview takes them when it starts.
 */
export function autoApplyDue(commands: ReturnType<typeof usePreviewCommands>, headCommit: string): boolean {
  if (!commands.configurationReady || !commands.allowed.apply) return false;
  if (commands.status === "PREVIEW_STATUS_READY") return (commands.configuration?.unappliedChangeCount ?? 0) > 0;
  const deployment = commands.preview?.automaticDeployment;
  return deployment?.status === "PREVIEW_DEPLOYMENT_STATUS_INCOMPATIBLE" && deployment.commit === headCommit;
}

/**
 * Applies Setup's saved Preview settings on their own once the edits settle: one apply per burst of changes, and
 * never twice for the same revision, so a refused apply waits for the next change or the problem's Apply again
 * instead of repeating.
 */
export function useAutoApply(commands: ReturnType<typeof usePreviewCommands>, headCommit: string) {
  const due = autoApplyDue(commands, headCommit);
  const revision = commands.configuration?.revision ?? "";
  const tried = useRef("");
  const act = useRef(commands.act);
  useEffect(() => {
    act.current = commands.act;
  });
  useEffect(() => {
    if (!due || tried.current === revision) return;
    const timer = setTimeout(() => {
      tried.current = revision;
      void act.current("apply");
    }, SETTLE_MS);
    return () => clearTimeout(timer);
  }, [due, revision]);
}
