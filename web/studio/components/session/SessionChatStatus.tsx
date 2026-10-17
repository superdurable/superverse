// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { SessionConnectionIndicator, SessionDetails } from "./SessionStatusIndicators";

/**
 * Beside the chat's heading: live-update and workspace details.
 */
export function SessionChatStatus() {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <SessionConnectionIndicator />
      <SessionDetails />
    </div>
  );
}
