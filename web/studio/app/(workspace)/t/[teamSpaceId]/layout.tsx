// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import { TeamSpaceGate } from "@/components/teamspace/TeamSpaceGate";

// A TeamSpace's pages open once the TeamSpace the URL names is read; the workspace's TeamSpaceProvider reads it.
export default function TeamSpaceLayout({ children }: { children: ReactNode }) {
  return <TeamSpaceGate>{children}</TeamSpaceGate>;
}
