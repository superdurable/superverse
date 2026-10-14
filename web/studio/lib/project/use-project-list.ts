// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { listProjects } from "@/lib/api/project";
import { ApiError } from "@/lib/api/transport/api-error";
import { usePagedList } from "@/lib/browser/use-paged-list";
import { delay } from "@/lib/shared/async";

/** How long a new TeamSpace or membership may take to reach the read model that admits ListProjects. */
const ADMITTED_WITHIN_MILLISECONDS = 10_000;

/**
 * Every Project of the TeamSpace with the caller's role in each, 25 a page with Load more, newest first as
 * ListProjects returns them; deleted Projects are dropped. Without a TeamSpace nothing is read. A new
 * `reloadKey`, such as the page's path, reads the first page again and keeps the shown rows until it arrives.
 * A TeamSpace the read model does not show the caller yet (404 or 403) is read again for ten seconds.
 */
export function useProjectList(teamSpaceId: string | null, reloadKey: unknown) {
  return usePagedList({
    key: `projects:${teamSpaceId}`,
    enabled: teamSpaceId !== null,
    fetchPage: async (cursor, signal) => {
      const deadline = Date.now() + ADMITTED_WITHIN_MILLISECONDS;
      for (;;) {
        try {
          const page = await listProjects({ teamSpaceId: teamSpaceId ?? "", pageSize: 25, pageToken: cursor }, signal);
          return { items: page.projects, nextPageToken: page.nextPageToken };
        } catch (reason) {
          const absent = reason instanceof ApiError && (reason.status === 404 || reason.status === 403);
          if (!absent || signal.aborted || Date.now() >= deadline) throw reason;
          await delay(1000, signal);
        }
      }
    },
    idOf: (project) => project.projectId,
    keep: (project) => project.status !== "PROJECT_STATUS_DELETED",
    reloadKey,
  });
}
