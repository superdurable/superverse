// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { handleUnscopedFlowApiMock } from "@/mock/flow-api-mock";
import { isStudioMockEnabled } from "@/mock/enabled";

// The unscoped Dex API exists only for mock fixtures. Real Flow views always
// use a Project-scoped route, so production fails closed here.
async function fixtureResponse(request: Request, context: RouteContext<"/dex/api/[...path]">): Promise<Response> {
  if (!isStudioMockEnabled())
    return Response.json(
      { error: "Project-scoped Dex access is unavailable" },
      { status: 503, headers: { "cache-control": "no-store" } },
    );
  return handleUnscopedFlowApiMock(request, (await context.params).path);
}

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
export const GET = fixtureResponse;
export const POST = fixtureResponse;
export const PATCH = fixtureResponse;
