// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { usePathname } from "next/navigation";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import { getTeamSpace } from "@/lib/api/tenancy/team-space";
import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import {
  readStoredPreference,
  STORAGE_KEYS,
  subscribeStoredPreferences,
  writeStoredPreference,
} from "@/lib/browser/storage";
import type { TeamSpaceSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/TeamSpaceSnapshot";
import { delay } from "@/lib/shared/async";

/**
 * A TeamSpace created a moment ago, and its creator's membership, reach the read model within seconds; until then
 * its read is NotFound or refused, so both are asked again for this long.
 */
const APPEARS_WITHIN_MILLISECONDS = 10_000;

interface Read {
  teamSpaceId: string;
  teamSpace: TeamSpaceSnapshot__Output | null;
  /** NotFound past the time a new TeamSpace takes to appear, or a refused read. */
  missing: boolean;
  error: string;
}

interface CurrentTeamSpace {
  /** The TeamSpace the page works in; null before the account has one. */
  teamSpaceId: string | null;
  /** Its snapshot; null while it is read. */
  teamSpace: TeamSpaceSnapshot__Output | null;
  missing: boolean;
  error: string;
  /** The Builder names the open Project's TeamSpace, which its URL does not. */
  follow: (teamSpaceId: string) => void;
  /** A write answered with the TeamSpace as it is now, such as a rename. */
  accept: (teamSpace: TeamSpaceSnapshot__Output) => void;
  /** A member change answered with the TeamSpace's new revision, which the next rename is fenced on. */
  noteRevision: (revision: string) => void;
  reload: () => void;
}

const TeamSpaceContext = createContext<CurrentTeamSpace | undefined>(undefined);

function teamSpaceIdFromPath(pathname: string): string | null {
  const match = /^\/t\/([^/]+)/.exec(pathname);
  if (!match?.[1]) return null;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return match[1];
  }
}

/** The newer of two snapshots of one TeamSpace: the read model may answer with one older than a write's. */
function newer(current: TeamSpaceSnapshot__Output | null, next: TeamSpaceSnapshot__Output) {
  return current && BigInt(current.revision) > BigInt(next.revision) ? current : next;
}

/**
 * The TeamSpace the signed-in pages work in: the one a `/t/{id}` page names, else the open Project's, else the
 * one last open in this browser. One read per TeamSpace serves the sidebar and the TeamSpace's pages; a read for
 * another TeamSpace, or a late one, never replaces the current one.
 */
export function TeamSpaceProvider({ children }: { children: ReactNode }) {
  const fromPath = teamSpaceIdFromPath(usePathname());
  const [followed, setFollowed] = useState<string | null>(null);
  // Read after hydration: the server renders without it.
  const remembered = useSyncExternalStore(
    subscribeStoredPreferences,
    () => readStoredPreference(STORAGE_KEYS.teamSpace),
    () => null,
  );
  const teamSpaceId = fromPath ?? followed ?? remembered;
  const [read, setRead] = useState<Read | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (teamSpaceId) writeStoredPreference(STORAGE_KEYS.teamSpace, teamSpaceId);
  }, [teamSpaceId]);

  useEffect(() => {
    if (!teamSpaceId) return;
    const controller = new AbortController();
    const settle = (next: Omit<Read, "teamSpaceId">) => {
      if (!controller.signal.aborted)
        setRead((current) => ({
          teamSpaceId,
          ...next,
          teamSpace:
            next.teamSpace && current?.teamSpaceId === teamSpaceId
              ? newer(current.teamSpace, next.teamSpace)
              : next.teamSpace,
        }));
    };
    void (async () => {
      const deadline = Date.now() + APPEARS_WITHIN_MILLISECONDS;
      for (;;) {
        try {
          return settle({ teamSpace: await getTeamSpace(teamSpaceId, controller.signal), missing: false, error: "" });
        } catch (reason) {
          if (controller.signal.aborted) return;
          const absent = reason instanceof ApiError && (reason.status === 404 || reason.status === 403);
          if (absent && Date.now() < deadline) {
            await delay(1000, controller.signal);
            continue;
          }
          return settle({
            teamSpace: null,
            missing: absent,
            error: errorMessage(reason, "Could not read the TeamSpace"),
          });
        }
      }
    })();
    return () => controller.abort();
  }, [teamSpaceId, tick]);

  const follow = useCallback((id: string) => setFollowed(id), []);
  const accept = useCallback(
    (teamSpace: TeamSpaceSnapshot__Output) =>
      setRead((current) => ({
        teamSpaceId: teamSpace.teamSpaceId,
        teamSpace: current?.teamSpaceId === teamSpace.teamSpaceId ? newer(current.teamSpace, teamSpace) : teamSpace,
        missing: false,
        error: "",
      })),
    [],
  );
  const noteRevision = useCallback(
    (revision: string) =>
      setRead((current) =>
        current?.teamSpace && BigInt(revision) > BigInt(current.teamSpace.revision)
          ? { ...current, teamSpace: { ...current.teamSpace, revision } }
          : current,
      ),
    [],
  );
  const reload = useCallback(() => setTick((value) => value + 1), []);
  const shown = read?.teamSpaceId === teamSpaceId ? read : null;
  const value = useMemo<CurrentTeamSpace>(
    () => ({
      teamSpaceId,
      teamSpace: shown?.teamSpace ?? null,
      missing: shown?.missing ?? false,
      error: shown?.error ?? "",
      follow,
      accept,
      noteRevision,
      reload,
    }),
    [teamSpaceId, shown, follow, accept, noteRevision, reload],
  );
  return <TeamSpaceContext.Provider value={value}>{children}</TeamSpaceContext.Provider>;
}

export function useTeamSpace(): CurrentTeamSpace {
  const value = useContext(TeamSpaceContext);
  if (value === undefined) throw new Error("TeamSpaceProvider is missing");
  return value;
}
