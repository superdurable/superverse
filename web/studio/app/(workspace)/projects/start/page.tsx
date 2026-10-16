// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { redirect } from "next/navigation";
import { studioRoutes } from "@/lib/shared/studio-routes";

// A Project starts in a Teamspace: the earlier address leads to the Teamspace the person opens.
export default function ProjectStartRoute() {
  redirect(studioRoutes.home);
}
