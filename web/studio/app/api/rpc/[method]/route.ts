// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { grpcBackend } from "@/lib/server/grpc-client";
import { handleRpcRequest } from "@/lib/server/rpc/rpc-handler";
import { mockBackend } from "@/mock/backend/mock-backend";
import { isStudioMockEnabled } from "@/mock/enabled";

async function handle(request: Request, context: RouteContext<"/api/rpc/[method]">) {
  const backend = isStudioMockEnabled() ? mockBackend(new URL(request.url).origin) : grpcBackend;
  return handleRpcRequest(request, (await context.params).method, backend);
}

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
export const GET = handle;
export const POST = handle;
// Every other verb reaches the handler too, which answers 405 with the verb this RPC accepts.
export const PUT = handle;
export const PATCH = handle;
export const DELETE = handle;
export const HEAD = handle;
export const OPTIONS = handle;
