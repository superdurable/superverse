// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { listTeamSpaces } from "@/lib/api/tenancy/team-space";
import { usePagedList } from "@/lib/browser/use-paged-list";

/**
 * The caller's Teamspaces, every one for the platform admin, 25 a page with Load more. A new `reloadKey` reads
 * the first page again and keeps the shown rows until it arrives.
 */
export function useTeamSpaceList(reloadKey: unknown) {
  return usePagedList({
    key: "team-spaces",
    fetchPage: async (cursor, signal) => {
      const page = await listTeamSpaces({ pageSize: 25, pageToken: cursor }, signal);
      return { items: page.teamSpaces, nextPageToken: page.nextPageToken };
    },
    idOf: (teamSpace) => teamSpace.teamSpaceId,
    reloadKey,
  });
}
