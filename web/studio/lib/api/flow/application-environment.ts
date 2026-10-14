// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readFlowApiJson } from "@/lib/api/transport/api-error";
import { sendFlowApiJson, type FlowApiClient } from "@/lib/api/transport/flow-api-client";
import { revisionFenceHeaders } from "./connector";

/** The environment settings the application declares, read and saved through the ApplicationEnvironmentService. */

export interface ApplicationEnvironmentField {
  name: string;
  required: boolean;
  secret: boolean;
  minLength: number;
  enum: string[];
  value?: string;
  configured: boolean;
}

export interface ApplicationEnvironmentSnapshot {
  appManifestRevision: number;
  configurationRevision: number;
  csrfToken: string;
  fields: ApplicationEnvironmentField[];
  obsolete: string[];
}

/**
 * Ordinary values to save, private values to replace and saved names to remove; on Live's next-publish
 * layer, `followPreview` names the settings whose own value goes, so they follow Preview again.
 */
export interface ApplicationEnvironmentWrite {
  values: Record<string, string>;
  secrets: Record<string, string>;
  remove: string[];
  followPreview?: string[];
}

export async function getApplicationEnvironment(
  client: FlowApiClient,
  signal?: AbortSignal,
): Promise<ApplicationEnvironmentSnapshot> {
  return readFlowApiJson<ApplicationEnvironmentSnapshot>(
    await client.fetch("/api/v2/application-environment", { signal }),
  );
}

/** Saves a write against the revisions of the snapshot it was made from; the answer is the new snapshot. */
export function saveApplicationEnvironment(
  client: FlowApiClient,
  snapshot: ApplicationEnvironmentSnapshot,
  definitionRevision: string,
  write: ApplicationEnvironmentWrite,
): Promise<ApplicationEnvironmentSnapshot> {
  return sendFlowApiJson<ApplicationEnvironmentSnapshot>(client, "/api/v2/application-environment", "PUT", write, {
    definitionRevision,
    headers: revisionFenceHeaders({
      csrfToken: snapshot.csrfToken,
      appManifestRevision: snapshot.appManifestRevision,
      configurationRevision: snapshot.configurationRevision,
    }),
  });
}
