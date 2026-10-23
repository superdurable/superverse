// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readUnary } from "./transport/read-unary";
import { unary } from "./transport/unary";

import { ModelProvider } from "@/lib/generated/protobuf/superverse/v2/ModelProvider";
import type { ModelCredentialInfo__Output } from "@/lib/generated/protobuf/superverse/v2/ModelCredentialInfo";
export { ModelCredentialStatus } from "@/lib/generated/protobuf/superverse/v2/ModelCredentialStatus";
export type CredentialInfo = ModelCredentialInfo__Output;
export { ModelProvider };
function info(value: CredentialInfo | null): CredentialInfo {
  if (!value) throw new Error("Missing credential metadata");
  return value;
}
export async function getModelCredential(projectId: string, provider: ModelProvider, signal: AbortSignal) {
  const response = await readUnary("GetModelCredentialInfo", { projectId, provider }, { signal });
  return info(response.credential);
}
export async function saveModelCredential(
  projectId: string,
  provider: ModelProvider,
  revision: string,
  requestId: string,
  apiKey: string,
  signal: AbortSignal,
  endpoint?: { baseUrl: string; customModelId: string; contextWindow: string; maxOutputTokens: string },
) {
  const response = await unary(
    "SetModelCredential",
    { projectId, provider, expectedRevision: revision, requestId, apiKey, ...endpoint },
    { signal },
  );
  return info(response.credential);
}
export async function deleteModelCredential(
  projectId: string,
  provider: ModelProvider,
  revision: string,
  requestId: string,
  signal: AbortSignal,
) {
  const response = await unary(
    "DeleteModelCredential",
    { projectId, provider, expectedRevision: revision, requestId },
    { signal },
  );
  return info(response.credential);
}

/** Fixed non-secret placeholder accepted by the local mock model endpoint. */
export const MOCK_MODEL_API_KEY = "mock-model-not-a-secret";

/** User-triggered authenticated discovery; passwords remain transient. */
export function listModelCredentialModels(
  projectId: string,
  provider: ModelProvider,
  baseUrl: string,
  apiKey: string,
  expectedRevision: string,
  signal: AbortSignal,
) {
  return unary("ListModelCredentialModels", { projectId, provider, baseUrl, apiKey, expectedRevision }, { signal });
}
