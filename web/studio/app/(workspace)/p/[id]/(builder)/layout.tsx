// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { ProjectResourceProvider } from "@/lib/project/ProjectResourceProvider";
import { ProjectBuilder } from "@/components/project/ProjectBuilder";
import { ProjectSessionProvider } from "@/components/project/ProjectSessionProvider";
import { MockFrame } from "@/mock/surface/MockFrame";
import { isStudioMockEnabled } from "@/mock/enabled";

// The Builder's frame, its Project and its Session outlive a move between surfaces: this layout
// stays mounted while only the [surface] page below it changes.
export default async function BuilderLayout({ children, params }: LayoutProps<"/p/[id]">) {
  const { id } = await params;
  return (
    <ProjectResourceProvider projectId={id}>
      <ProjectSessionProvider projectId={id}>
        {isStudioMockEnabled() ? (
          <MockFrame projectId={id}>{children}</MockFrame>
        ) : (
          <ProjectBuilder projectId={id}>{children}</ProjectBuilder>
        )}
      </ProjectSessionProvider>
    </ProjectResourceProvider>
  );
}
