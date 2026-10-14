// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { PROJECT_ROLES, type ProjectRole } from "@/lib/teamspace/project-roles";
import { cx } from "@/components/ui/cx";

/**
 * What the person does in a Project, as the bar, the project switch, the sidebar and Home show it beside the
 * Project's name: building in the accent hue, and "not joined" without a role.
 */
export function ProjectRoleChip({ role, className }: { role: ProjectRole | null; className?: string }) {
  const word = role ? PROJECT_ROLES[role].word : "not joined";
  return (
    <span
      title={role ? `You ${word} here` : "You have no role in this project; an owner can add you"}
      className={cx(
        "inline-flex h-5 shrink-0 items-center rounded-full border px-2 text-[11.5px] font-medium whitespace-nowrap",
        role && PROJECT_ROLES[role].builds ? "border-accent-rule text-accent" : "border-line-soft text-ink-dim",
        !role && "border-dashed text-ink-faint",
        className,
      )}
    >
      {word}
    </span>
  );
}
