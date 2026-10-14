// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { listDeployments } from "@/lib/api/deployment";
import { listReleases } from "@/lib/api/release";
import { timestampToISO } from "@/lib/shared/protobuf";
import { newestPublish, type NewestPublish } from "./environment-status";

/** How many of the newest deployments and Releases the Live status looks at. */
const PAGE_SIZE = 50;

const began = (value: { createdAt: Parameters<typeof timestampToISO>[0] }) =>
  Date.parse(timestampToISO(value.createdAt) ?? "") || 0;

/**
 * The Project's newest build or deployment, read again whenever `reloadKey` changes, such as the Live
 * pointer's revision; null without a Project (an operator reads none) or while it is read.
 */
export function useNewestPublish(projectId: string | null, reloadKey: string): NewestPublish | null {
  const [read, setRead] = useState<{ key: string; newest: NewestPublish | null } | null>(null);
  const key = JSON.stringify([projectId, reloadKey]);
  useEffect(() => {
    if (projectId === null) return;
    const controller = new AbortController();
    Promise.all([
      listDeployments({ projectId, pageSize: PAGE_SIZE }, controller.signal),
      listReleases({ projectId, pageSize: PAGE_SIZE }, controller.signal),
    ]).then(
      ([deployments, releases]) => {
        if (controller.signal.aborted) return;
        const deployment = deployments.deployments.toSorted((a, b) => began(b) - began(a))[0] ?? null;
        const build =
          releases.releases.flatMap((release) => release.buildRevisions).toSorted((a, b) => began(b) - began(a))[0] ??
          null;
        setRead({ key, newest: newestPublish(build, deployment) });
      },
      // An unreadable history leaves the status to the Live pointer alone.
      () => !controller.signal.aborted && setRead({ key, newest: null }),
    );
    return () => controller.abort();
  }, [key, projectId]);
  return read?.key === key ? read.newest : null;
}
