// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { errorResponse } from "@/lib/server/grpc-client";
import { completeHandoff } from "@/lib/server/login-host/handoff-handler";

export async function GET(request: Request) {
  try {
    return await completeHandoff(request);
  } catch (error) {
    return errorResponse(error);
  }
}

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
