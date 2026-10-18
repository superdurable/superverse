// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { isStudioMockEnabled } from "@/mock/enabled";
import { mockAttachmentFile, putMockUpload } from "@/mock/store/attachments/mock-attachments";
import { getStudioMockStore } from "@/mock/store/store";

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

// The mock's stand-in for storage's presigned URLs, which exists only in mock mode: the PUT of an upload and
// the GET of a download. The mock signs nothing; the attachment ID and its state admit each request.

export async function PUT(request: Request, context: RouteContext<"/api/mock-attachments/[attachmentId]">) {
  if (!isStudioMockEnabled()) return new Response(null, { status: 404 });
  const store = getStudioMockStore(new URL(request.url).origin);
  const body = Buffer.from(await request.arrayBuffer());
  const status = putMockUpload(store, (await context.params).attachmentId, request.headers, body);
  return new Response(null, { status });
}

export async function GET(request: Request, context: RouteContext<"/api/mock-attachments/[attachmentId]">) {
  if (!isStudioMockEnabled()) return new Response(null, { status: 404 });
  const url = new URL(request.url);
  const store = getStudioMockStore(url.origin);
  const file = mockAttachmentFile(store, (await context.params).attachmentId, url.searchParams.get("project") ?? "");
  if (!file) return new Response(null, { status: 404 });
  const disposition = url.searchParams.get("disposition") === "inline" ? "inline" : "attachment";
  return new Response(new Uint8Array(file.bytes), {
    headers: {
      "content-type": file.mediaType,
      "content-disposition": `${disposition}; filename*=UTF-8''${encodeURIComponent(file.fileName)}`,
      "cache-control": "private, max-age=300",
    },
  });
}
