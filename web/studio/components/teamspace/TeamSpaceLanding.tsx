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
import { useAccount } from "@/lib/account/AccountProvider";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import { useTeamSpaceList } from "@/lib/teamspace/use-team-space-list";
import { NotReady } from "@/components/ui/NotReady";
import { TeamSpaceCreateForm, teamSpaceNameFromEmail, useTeamSpaceCreation } from "./TeamSpaceCreateForm";

/**
 * The request ID of an account's first Teamspace. The server keys a creation by the account and its request,
 * so every tab, reload and retry of it creates the same one Teamspace.
 */
const FIRST_TEAMSPACE_REQUEST = "first-teamspace";

/**
 * `/`: opens the Teamspace last open in this browser when the person still has it, else their first one. An
 * account without a Teamspace gets its first one here, named from its email, without a question: every Project
 * belongs to one. Should that fail, the form to name it takes over with the name it tried.
 */
export function TeamSpaceLanding() {
  const router = useRouter();
  const account = useAccount();
  const { teamSpaceId } = useTeamSpace();
  const list = useTeamSpaceList("landing");
  const target = list.loaded
    ? (list.items.find((item) => item.teamSpaceId === teamSpaceId) ?? list.items[0] ?? null)
    : null;
  const firstName = teamSpaceNameFromEmail(account?.email ?? "");
  const first = { name: firstName, requestId: FIRST_TEAMSPACE_REQUEST };
  const creation = useTeamSpaceCreation(first);
  const { create } = creation;
  const creates = list.loaded && !list.error && list.items.length === 0 && firstName !== "";
  useEffect(() => {
    if (target) router.replace(studioRoutes.teamSpace(target.teamSpaceId));
  }, [router, target]);
  useEffect(() => {
    if (creates) create(firstName);
  }, [create, creates, firstName]);

  if (list.error)
    return (
      <NotReady kind="error" heading="Your Teamspaces did not load" retry>
        {list.error}
      </NotReady>
    );
  if (!list.loaded || target)
    return (
      <NotReady kind="placeholder" heading="Opening your Teamspace">
        Reading the Teamspaces you belong to…
      </NotReady>
    );
  if (creates && !creation.error)
    return (
      <NotReady kind="placeholder" heading="Setting up your Teamspace…">
        {`Creating ${firstName}, where your projects live. You can rename it on Home.`}
      </NotReady>
    );
  return (
    <main className="flex min-h-0 min-w-0 flex-1 items-center-safe justify-center-safe overflow-y-auto p-8 max-sm:p-4">
      <div className="w-full max-w-md py-8">
        <h1 className="text-3xl font-semibold tracking-tight">Create your Teamspace.</h1>
        <p className="mt-3 mb-6 text-base leading-relaxed text-ink-dim">
          A Teamspace holds a team&apos;s projects and people. Create one to start your first project, or ask an admin
          of an existing Teamspace to add you.
        </p>
        <TeamSpaceCreateForm first={creation.error ? { ...first, error: creation.error } : undefined} />
      </div>
    </main>
  );
}
