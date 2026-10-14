// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { PROJECT_ROLES, type ProjectRole } from "@/lib/teamspace/project-roles";

// The mock project surface's role lens, `?role=`: it only chooses which sample surfaces appear,
// through the surface registry; it is never authorization. Without one the Project's own role applies.
export function resolveRole(value: string | undefined): ProjectRole | undefined {
  return value !== undefined && Object.hasOwn(PROJECT_ROLES, value) ? (value as ProjectRole) : undefined;
}
