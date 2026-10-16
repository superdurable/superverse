// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ErrorInfo__Output } from "@/lib/generated/protobuf/google/rpc/ErrorInfo";
import { FailureRemedy } from "@/lib/generated/protobuf/superverse/v2/FailureRemedy";
import { isRecord } from "@/lib/shared/json";

/** A build cancellation refused because its automatic deployment may be admitted; cancel that instead. */
const RELEASE_BUILD_DEPLOYING = "RELEASE_BUILD_DEPLOYING";
/** The ErrorInfo metadata key of the deployment RELEASE_BUILD_DEPLOYING names (Go's deploymentMetadataKey). */
const DEPLOYMENT_KEY = "deploymentId";
/** A deployment ID is a lowercase DNS label of 1 to 36 characters, as StartDeployment requires. */
const DEPLOYMENT_ID = /^[a-z0-9](?:[a-z0-9-]{0,34}[a-z0-9])?$/;

/**
 * The refusals of a Teamspace connection request that Studio branches on, as Go names them in the
 * ErrorInfo reason (internal/tenancy/connections RefusalReason, and DEPLOYMENT_IN_PROGRESS of a
 * release of unused connections), each with what Studio tells the person.
 */
const CONNECTION_REFUSALS = {
  CONNECTOR_VERSION_UNSUPPORTED: "This connector version predates Teamspace connections; use a newer release.",
  CONNECTION_SCOPES_DIFFER: "The connection was authorized for other OAuth scopes; reauthorize it for this version.",
  CONNECTION_DELETED: "The connection was deleted.",
  CONNECTION_IN_USE: "A Project still uses this connection.",
  CONNECTION_NAME_TAKEN: "Another connection of this Teamspace has that name.",
  OAUTH_DENIED: "The provider denied the authorization.",
  DEPLOYMENT_IN_PROGRESS: "A deployment is in progress; release unused connections after it.",
} as const;

export type ConnectionRefusal = keyof typeof CONNECTION_REFUSALS;

/** The connection refusal `value`, an ErrorInfo as the BFF or the browser received it, names, if any. */
function connectionRefusal(value: unknown): ConnectionRefusal | undefined {
  if (
    isRecord(value) &&
    value.domain === "superverse.v2" &&
    typeof value.reason === "string" &&
    Object.hasOwn(CONNECTION_REFUSALS, value.reason)
  )
    return value.reason as ConnectionRefusal;
}

/** The ErrorInfo of a connection refusal, without metadata, as the BFF lets it cross. */
export function connectionRefusalInfo(value: unknown): ErrorInfo__Output | undefined {
  const reason = connectionRefusal(value);
  return reason && { reason, domain: "superverse.v2", metadata: {} };
}

/** What Studio tells the person about a refusal reason, such as an authorization's `reason`; undefined for others. */
export function connectionRefusalText(reason: string): string | undefined {
  return Object.hasOwn(CONNECTION_REFUSALS, reason) ? CONNECTION_REFUSALS[reason as ConnectionRefusal] : undefined;
}

/**
 * The refusals of a Release build's admission (STUDIOSETUP PUBGATE) and of its cancellation (decision
 * 0027), which the server declares with their remedies; their message says why.
 */
const RELEASE_REFUSALS: ReadonlySet<unknown> = new Set([
  "PREVIEW_VALIDATION_REQUIRED",
  "RELEASE_CONFIGURATION_STALE",
  "RELEASE_BUILD_NOT_CANCELLABLE",
  RELEASE_BUILD_DEPLOYING,
]);

/**
 * The ErrorInfo of a Release refusal, as the BFF lets it cross: RELEASE_BUILD_DEPLOYING also carries the
 * `deploymentId` it names, the deployment its remedy cancels, when that is a deployment ID.
 */
export function releaseRefusalInfo(value: unknown): ErrorInfo__Output | undefined {
  const info = reasonInfo(value, RELEASE_REFUSALS);
  const deploymentId = isRecord(value) && isRecord(value.metadata) ? value.metadata[DEPLOYMENT_KEY] : undefined;
  if (info?.reason !== RELEASE_BUILD_DEPLOYING || typeof deploymentId !== "string" || !DEPLOYMENT_ID.test(deploymentId))
    return info;
  return { ...info, metadata: { [DEPLOYMENT_KEY]: deploymentId } };
}

/** The deployment a refused build cancellation names, which Studio cancels instead; empty when none. */
export function deployingRefusalDeployment(errorInfo: ErrorInfo__Output | undefined): string {
  return errorInfo?.reason === RELEASE_BUILD_DEPLOYING ? (errorInfo.metadata?.[DEPLOYMENT_KEY] ?? "") : "";
}

/** The refusals of a deployment's cancellation, which the server declares with their remedies. */
const DEPLOYMENT_REFUSALS: ReadonlySet<unknown> = new Set(["DEPLOYMENT_NOT_CANCELLABLE"]);

/** The ErrorInfo of a deployment refusal, as the BFF lets it cross. */
export function deploymentRefusalInfo(value: unknown): ErrorInfo__Output | undefined {
  return reasonInfo(value, DEPLOYMENT_REFUSALS);
}

/**
 * The refusals of the Preview and of a Project configuration that any method may answer with
 * (internal/api/boundary error_reasons.go), each with the remedy the server declares.
 */
const OPERATION_REFUSALS: ReadonlySet<unknown> = new Set([
  "PREVIEW_UNAVAILABLE",
  "PREVIEW_SOURCE_NOT_PREPARED",
  "CONFIGURATION_NOT_READY",
  "CONFIGURATION_INVALID",
]);

/** The ErrorInfo of a Preview or configuration refusal, as the BFF lets it cross. */
export function operationRefusalInfo(value: unknown): ErrorInfo__Output | undefined {
  return reasonInfo(value, OPERATION_REFUSALS);
}

function reasonInfo(value: unknown, reasons: ReadonlySet<unknown>): ErrorInfo__Output | undefined {
  if (isRecord(value) && value.domain === "superverse.v2" && reasons.has(value.reason))
    return { reason: value.reason as string, domain: "superverse.v2", metadata: {} };
}

/**
 * info as it crosses to the browser with the remedy the server declared with the reason in its
 * ErrorInfo `original`, when it names a FailureRemedy value, beside any metadata info already let cross.
 */
export function withDeclaredRemedy(info: ErrorInfo__Output, original: unknown): ErrorInfo__Output {
  const remedy = isRecord(original) && isRecord(original.metadata) ? original.metadata.remedy : undefined;
  if (typeof remedy !== "string" || !Object.hasOwn(FailureRemedy, remedy)) return info;
  return { ...info, metadata: { ...info.metadata, remedy } };
}
