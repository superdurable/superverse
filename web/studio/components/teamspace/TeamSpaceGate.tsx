// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { ReactNode } from "react";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import { NotReady } from "@/components/ui/NotReady";

/**
 * The pages of one Teamspace, once it is read: a Teamspace created a moment ago is waited for, one the person
 * cannot open or that is gone says so, and one being deleted shows nothing else.
 */
export function TeamSpaceGate({ children }: { children: ReactNode }) {
  const { teamSpace, missing, error } = useTeamSpace();
  const elsewhere = { label: "Open your Teamspaces", href: studioRoutes.home };
  if (missing)
    return (
      <NotReady kind="unavailable" heading="This Teamspace is not open to you" action={elsewhere}>
        It does not exist, it was deleted, or you are not one of its members. An admin of the Teamspace can add you.
      </NotReady>
    );
  if (error)
    return (
      <NotReady kind="error" heading="This Teamspace did not load" retry>
        {error}
      </NotReady>
    );
  if (!teamSpace)
    return (
      <NotReady kind="placeholder" heading="Opening the Teamspace">
        Reading the Teamspace…
      </NotReady>
    );
  if (teamSpace.status === "TEAM_SPACE_STATUS_DELETING")
    return (
      <NotReady kind="unavailable" heading={`${teamSpace.name} is being deleted`} action={elsewhere}>
        Its projects and their Live apps are deleted with it. Nothing in it can change any more.
      </NotReady>
    );
  return children;
}
