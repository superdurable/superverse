// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "@/lib/api/transport/api-error";

/**
 * At most one user action of a component in flight. A second click while one
 * runs is ignored, the action's failure becomes `error`, with the action and
 * its reason as `failure` for the problem panel, and unmounting aborts the
 * action (its work sees an aborted signal, which is an uncertain outcome).
 */
export function useSingleFlight<Label extends string>() {
  const [busy, setBusy] = useState<Label | null>(null);
  const [error, setError] = useState("");
  const [failure, setFailure] = useState<{ label: Label; reason: unknown } | null>(null);
  const current = useRef<AbortController | null>(null);

  // Unmounting abandons the action. React may mount the component again (Strict
  // Mode replays effects), so the abandoned action must not keep it busy.
  useEffect(
    () => () => {
      current.current?.abort();
      current.current = null;
      setBusy(null);
    },
    [],
  );

  const run = useCallback(async (label: Label, work: (signal: AbortSignal) => Promise<void>) => {
    if (current.current) return;
    const controller = new AbortController();
    current.current = controller;
    setBusy(label);
    setError("");
    setFailure(null);
    try {
      await work(controller.signal);
    } catch (reason) {
      if (!controller.signal.aborted) {
        setError(errorMessage(reason, "Request failed"));
        setFailure({ label, reason });
      }
    } finally {
      if (current.current === controller) {
        current.current = null;
        setBusy(null);
      }
    }
  }, []);

  return { busy, error, failure, setError, run };
}
