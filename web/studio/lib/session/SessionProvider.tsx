// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useSessionOwner } from "./use-session-owner";

type SessionContextValue = ReturnType<typeof useSessionOwner>;
type SessionResourceValue = Pick<
  SessionContextValue,
  "projectId" | "resource" | "status" | "loading" | "error" | "streamState" | "refresh"
>;
const SessionContext = createContext<SessionContextValue | null>(null);
const SessionResourceContext = createContext<SessionResourceValue | null>(null);

function SessionOwner({ projectId, children }: { projectId: string; children: ReactNode }) {
  const session = useSessionOwner(projectId);
  const { resource, status, loading, error, streamState, refresh } = session;
  // Streamed text changes only the conversation context. Resource views and
  // indicators read this stable selection of the same owner.
  const selected = useMemo(
    () => ({ projectId, resource, status, loading, error, streamState, refresh }),
    [projectId, resource, status, loading, error, streamState, refresh],
  );
  return (
    <SessionResourceContext.Provider value={selected}>
      <SessionContext.Provider value={session}>{children}</SessionContext.Provider>
    </SessionResourceContext.Provider>
  );
}

/**
 * A Project page owns its Project's one Session, which ProjectFlow starts once
 * the Project is READY: the caller makes the provider active then. Until then
 * the page has no Session, and the hooks below read null.
 */
export function SessionProvider({
  projectId,
  active,
  children,
}: {
  projectId: string;
  active: boolean;
  children: ReactNode;
}) {
  return active ? (
    <SessionOwner key={projectId} projectId={projectId}>
      {children}
    </SessionOwner>
  ) : (
    children
  );
}

/** The page Session's resource, or null while the page has no Session. */
export function useSessionResource() {
  return useContext(SessionResourceContext);
}

/** The page Session's resource, for a component that renders only with the Session. */
export function useProjectSessionResource() {
  const value = useSessionResource();
  if (!value) throw new Error("Session must use its page provider");
  return value;
}

/** The page Session with its commands, for a component that renders only with the Session. */
export function useProjectSession() {
  const value = useContext(SessionContext);
  if (!value) throw new Error("Session must use its page provider");
  return value;
}
