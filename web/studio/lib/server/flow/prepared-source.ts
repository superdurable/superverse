// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { callerFromRequest } from "@/lib/server/caller-envelope";
import { errorResponse, type RpcBackend } from "@/lib/server/grpc-client";
import { buildDefinitionDexCatalog } from "@/lib/shared/flow-definition-catalog";
import { decodeStruct } from "@/lib/shared/protobuf";

/** The Project whose prepared Preview source a /dex/projects/{project}/source route reads. */
export interface PreparedSourceSelection {
  projectId: string;
  path: string[];
}

const DOCUMENTS = ["flow-definitions", "v2/catalog"];

/** The read-only catalog of the Project's prepared Preview source: Build's Design. */
export async function handlePreparedSourceRequest(
  request: Request,
  { projectId, path }: PreparedSourceSelection,
  backend: RpcBackend,
): Promise<Response> {
  try {
    // A prepared source serves only Build's Design.
    if (!DOCUMENTS.includes(path.join("/")))
      return Response.json({ error: "Prepared source has no running application" }, { status: 404 });
    const { preview, flowDefinitions } = await backend.unary(
      "GetPreviewDefinitions",
      { projectId },
      callerFromRequest(request),
      request.signal,
    );
    const prepared = preview?.preparedSource;
    if (
      preview?.projectId !== projectId ||
      prepared?.status !== "PREVIEW_SOURCE_STATUS_READY" ||
      !prepared.definitionRevision
    ) {
      return Response.json({ error: "Prepare Preview source in Build" }, { status: 409 });
    }
    const catalog = buildDefinitionDexCatalog(
      prepared.definitionRevision,
      flowDefinitions.map((item) => ({ ...item, graph: decodeStruct(item.definition) })),
      "session-preview-source",
    );
    return Response.json(path.join("/") === "flow-definitions" ? catalog.definitions : catalog.catalog, {
      headers: { "cache-control": "no-store", "x-content-type-options": "nosniff" },
    });
  } catch (error) {
    return errorResponse(error);
  }
}
