// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { errorResponse } from "@/lib/server/grpc-client";
import { authorizeLogin, prepareLogin } from "@/lib/server/login-host/handoff-handler";

export function GET(request: Request) {
  try {
    return prepareLogin(request);
  } catch (error) {
    return errorResponse(error);
  }
}

export async function POST(request: Request) {
  try {
    return await authorizeLogin(request);
  } catch (error) {
    return errorResponse(error);
  }
}

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
