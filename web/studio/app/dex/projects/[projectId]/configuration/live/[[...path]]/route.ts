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

// Live's configuration: a version's frozen copy, /configuration/live/releases/{commit}/..., read only, or Live's
// next-publish layer, /configuration/live/next/..., checked against the Preview's source, or
// /configuration/live/next/releases/{commit}/..., checked against that Release's.
async function handle(
  request: Request,
  context: RouteContext<"/dex/projects/[projectId]/configuration/live/[[...path]]">,
) {
  const { projectId, path = [] } = await context.params;
  const nextPublish = path[0] === "next";
  const rest = nextPublish ? path.slice(1) : path;
  const named = rest[0] === "releases" && Boolean(rest[1]);
  if (!nextPublish && !named)
    return Response.json({ error: "Select a commit in Publishing Configuration" }, { status: 409 });
  return (isStudioMockEnabled() ? handleFlowApiMock : handleFlowApiRequest)(request, {
    projectId,
    nextPublish,
    releaseId: named ? rest[1] : "",
    path: named ? rest.slice(2) : rest,
    purpose: "configuration",
  });
}

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
export const GET = handle;
export const HEAD = handle;
export const POST = handle;
export const PUT = handle;
export const DELETE = handle;
