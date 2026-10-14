// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { isStudioMockEnabled } from "@/mock/enabled";
import { mockAppResponse } from "@/mock/mock-app";

export const dynamic = "force-dynamic";

// The stand-in application exists only in mock mode.
export async function GET(request: Request) {
  return isStudioMockEnabled() ? mockAppResponse(request) : new Response(null, { status: 404 });
}
