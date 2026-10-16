// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { listProjectMembers } from "@/lib/api/tenancy/project-member";
import { listTeamSpaceMembers } from "@/lib/api/tenancy/team-space-member";
import { errorMessage } from "@/lib/api/transport/api-error";

/**
 * One member list, read once per key and again on `reload`; a change's answer replaces it at once. A read for
 * another key, or one that a change overtook, never replaces what is shown.
 */
function useMemberList<T>(key: string | null, read: (signal: AbortSignal) => Promise<T[]>) {
  const [list, setList] = useState<{ key: string; members: T[]; error: string } | null>(null);
  const [tick, setTick] = useState(0);
  const reader = useRef(read);
  // Changes made since a read started, which its answer would undo.
  const changes = useRef(0);
  useEffect(() => {
    reader.current = read;
  });
  useEffect(() => {
    if (key === null) return;
    const controller = new AbortController();
    const started = changes.current;
    reader.current(controller.signal).then(
      (members) => {
        if (!controller.signal.aborted && changes.current === started) setList({ key, members, error: "" });
      },
      (reason: unknown) => {
        if (!controller.signal.aborted)
          setList((current) => ({
            key,
            members: current?.key === key ? current.members : [],
            error: errorMessage(reason, "Could not read the members"),
          }));
      },
    );
    return () => controller.abort();
  }, [key, tick]);
  const replace = useCallback(
    (members: T[]) => {
      if (key === null) return;
      changes.current += 1;
      setList({ key, members, error: "" });
    },
    [key],
  );
  const reload = useCallback(() => setTick((value) => value + 1), []);
  const shown = list?.key === key ? list : null;
  return { members: shown?.members ?? null, error: shown?.error ?? "", replace, reload };
}

/** The Teamspace's members, which every member may read. */
export function useTeamSpaceMembers(teamSpaceId: string | null) {
  return useMemberList(teamSpaceId, (signal) => listTeamSpaceMembers(teamSpaceId ?? "", signal));
}

/** The Project's members, which every role in it may read. */
export function useProjectMembers(projectId: string | null) {
  return useMemberList(projectId, (signal) => listProjectMembers(projectId ?? "", signal));
}
