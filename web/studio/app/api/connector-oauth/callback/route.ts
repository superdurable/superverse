// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { completeConnectorOAuth } from "@/lib/server/connector-oauth/connector-oauth-handler";
import { grpcBackend } from "@/lib/server/grpc-client";
import { mockBackend } from "@/mock/backend/mock-backend";
import { isStudioMockEnabled } from "@/mock/enabled";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";
export async function GET(request: Request) {
  return completeConnectorOAuth(
    request,
    isStudioMockEnabled() ? mockBackend(new URL(request.url).origin) : grpcBackend,
  );
}
