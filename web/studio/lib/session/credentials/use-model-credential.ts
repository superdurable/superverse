// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getModelCredential, type CredentialInfo } from "@/lib/api/model-credential";
import { isAbortError } from "@/lib/shared/async";

/** The page owns one non-secret key status; credentials never enter Session history or browser storage. */
export function useModelCredential(projectId: string, active: boolean, credentialVersion: string | undefined) {
  const [info, setInfo] = useState<CredentialInfo | null>(null);
  const [pending, setPending] = useState<{ kind: "save" | "delete"; revision: string; requestId: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const request = useRef<AbortController | null>(null);
  const seenVersion = useRef(credentialVersion);
  const refresh = useCallback(() => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    return getModelCredential(projectId, controller.signal).then(
      (next) => {
        if (controller.signal.aborted) return;
        setInfo(next);
        setPending(
          next.pendingOperation
            ? {
                kind: next.pendingDelete ? "delete" : "save",
                revision: next.pendingRevision,
                requestId: next.pendingOperation,
              }
            : null,
        );
        setError(null);
        return next;
      },
      (reason: unknown) => {
        if (!isAbortError(reason)) setError("Could not read your Anthropic key status.");
      },
    );
  }, [projectId]);
  useEffect(() => {
    if (!active) return;
    void refresh();
    const focus = () => void refresh();
    window.addEventListener("focus", focus);
    return () => {
      request.current?.abort();
      window.removeEventListener("focus", focus);
    };
  }, [active, refresh]);
  useEffect(() => {
    if (!active || !credentialVersion || credentialVersion === seenVersion.current) return;
    seenVersion.current = credentialVersion;
    void refresh();
  }, [active, credentialVersion, refresh]);
  return useMemo(() => ({ info, error, refresh, setInfo, pending, setPending }), [info, error, refresh, pending]);
}
