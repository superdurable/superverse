// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { ProjectStartForm } from "@/components/project/ProjectStartForm";
import { PageBar } from "@/components/shell/PageBar";

export default async function ProjectStartRoute({ params }: PageProps<"/t/[teamSpaceId]/projects/start">) {
  const { teamSpaceId } = await params;
  return (
    <>
      <PageBar title="New project" />
      <ProjectStartForm teamSpaceId={teamSpaceId} />
    </>
  );
}
