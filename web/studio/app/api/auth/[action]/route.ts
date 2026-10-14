// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { handleAuthRequest } from "@/lib/server/auth-handler";
import { grpcBackend } from "@/lib/server/grpc-client";
import { mockBackend } from "@/mock/backend/mock-backend";
import { isStudioMockEnabled } from "@/mock/enabled";

async function handle(request: Request, context: RouteContext<"/api/auth/[action]">) {
  const backend = isStudioMockEnabled() ? mockBackend(new URL(request.url).origin) : grpcBackend;
  return handleAuthRequest(request, (await context.params).action, backend);
}

export const GET = handle;
export const POST = handle;
export const dynamic = "force-dynamic";
export const runtime = "nodejs";
