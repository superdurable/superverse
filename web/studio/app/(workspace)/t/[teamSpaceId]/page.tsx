// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { ProjectHome } from "@/components/project/ProjectHome";
import { PageBar } from "@/components/shell/PageBar";
import { TeamSpaceOverview } from "@/components/teamspace/TeamSpaceOverview";

export default async function TeamSpaceHomePage({ params }: PageProps<"/t/[teamSpaceId]">) {
  const { teamSpaceId } = await params;
  return (
    <>
      <PageBar title="Home" />
      <TeamSpaceOverview />
      <ProjectHome teamSpaceId={teamSpaceId} />
    </>
  );
}
