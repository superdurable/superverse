// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import { useTeamSpaceList } from "@/lib/teamspace/use-team-space-list";
import { NotReady } from "@/components/ui/NotReady";
import { TeamSpaceCreateForm } from "./TeamSpaceCreateForm";

/**
 * `/`: opens the TeamSpace last open in this browser when the person still has it, else their first one; an
 * account without a TeamSpace creates its first one here.
 */
export function TeamSpaceLanding() {
  const router = useRouter();
  const { teamSpaceId } = useTeamSpace();
  const list = useTeamSpaceList("landing");
  const target = list.loaded
    ? (list.items.find((item) => item.teamSpaceId === teamSpaceId) ?? list.items[0] ?? null)
    : null;
  useEffect(() => {
    if (target) router.replace(studioRoutes.teamSpace(target.teamSpaceId));
  }, [router, target]);

  if (list.error)
    return (
      <NotReady kind="error" heading="Your TeamSpaces did not load" retry>
        {list.error}
      </NotReady>
    );
  if (!list.loaded || target)
    return (
      <NotReady kind="placeholder" heading="Opening your TeamSpace">
        Reading the TeamSpaces you belong to…
      </NotReady>
    );
  return (
    <main className="flex min-h-0 min-w-0 flex-1 items-center-safe justify-center-safe overflow-y-auto p-8 max-sm:p-4">
      <div className="w-full max-w-md py-8">
        <h1 className="text-3xl font-semibold tracking-tight">Create your TeamSpace.</h1>
        <p className="mt-3 mb-6 text-base leading-relaxed text-ink-dim">
          A TeamSpace holds a team&apos;s projects and people. Create one to start your first project, or ask an admin
          of an existing TeamSpace to add you.
        </p>
        <TeamSpaceCreateForm />
      </div>
    </main>
  );
}
