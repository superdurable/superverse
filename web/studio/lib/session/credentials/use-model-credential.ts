// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getModelCredential, type ModelProvider, type CredentialInfo } from "@/lib/api/model-credential";
import { isAbortError } from "@/lib/shared/async";

/** The page owns one non-secret key status; credentials never enter Session history or browser storage. */
export function useModelCredential(
  projectId: string,
  provider: ModelProvider,
  active: boolean,
  credentialVersion: string | undefined,
) {
  const key = `${projectId}/${provider}`;
  const [rows, setRows] = useState<Record<string, CredentialInfo | null>>({});
  const info = rows[key] ?? null;
  const setInfo = useCallback(
    (value: CredentialInfo) => {
      setRows((current) => ({ ...current, [key]: value }));
    },
    [key],
  );
  type Pending = { kind: "save" | "delete"; revision: string; requestId: string };
  const [operations, setOperations] = useState<Record<string, Pending | null>>({});
  const pending = operations[key] ?? null;
  const setPending = useCallback(
    (value: Pending | null) => {
      setOperations((current) => ({ ...current, [key]: value }));
    },
    [key],
  );
  const [errors, setErrors] = useState<Record<string, string | null>>({});
  const error = errors[key] ?? null;
  const setError = useCallback(
    (value: string | null) => {
      setErrors((current) => ({ ...current, [key]: value }));
    },
    [key],
  );
  const request = useRef<AbortController | null>(null);
  const seenVersion = useRef(credentialVersion);
  const refresh = useCallback(() => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    return getModelCredential(projectId, provider, controller.signal).then(
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
        if (!isAbortError(reason) && !controller.signal.aborted)
          setError("Could not read the selected provider key status.");
      },
    );
  }, [projectId, provider, setError, setInfo, setPending]);
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
  return useMemo(
    () => ({ provider, info, error, refresh, setInfo, pending, setPending }),
    [provider, info, error, refresh, setInfo, pending, setPending],
  );
}
