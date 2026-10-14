// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { handleFlowApiRequest } from "@/lib/server/flow/flow-api-handler";
import { isStudioMockEnabled } from "@/mock/enabled";
import { handleFlowApiMock } from "@/mock/flow-api-mock";

// Preview Runs and Inbox: the running Preview of the Project.
async function handle(
  request: Request,
  context: RouteContext<"/dex/projects/[projectId]/environments/preview/[[...path]]">,
): Promise<Response> {
  return (isStudioMockEnabled() ? handleFlowApiMock : handleFlowApiRequest)(request, {
    ...(await context.params),
    preview: true,
    purpose: "runs",
  });
}

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
export const GET = handle;
export const HEAD = handle;
export const POST = handle;
export const PUT = handle;
export const PATCH = handle;
export const DELETE = handle;
