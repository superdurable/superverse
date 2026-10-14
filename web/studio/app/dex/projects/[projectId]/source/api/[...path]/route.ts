// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { handlePreparedSourceRequest } from "@/lib/server/flow/prepared-source";
import { grpcBackend } from "@/lib/server/grpc-client";
import { mockBackend } from "@/mock/backend/mock-backend";
import { isStudioMockEnabled } from "@/mock/enabled";

// Build's Design: the prepared Preview source of the Project's Session.
export async function GET(
  request: Request,
  context: RouteContext<"/dex/projects/[projectId]/source/api/[...path]">,
): Promise<Response> {
  const backend = isStudioMockEnabled() ? mockBackend(new URL(request.url).origin) : grpcBackend;
  return handlePreparedSourceRequest(request, await context.params, backend);
}

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
