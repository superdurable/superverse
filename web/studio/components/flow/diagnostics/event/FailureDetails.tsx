// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { backendErrorLabel, grpcStatusLabel } from "@/lib/flow/enum-labels";
import { asRecord, hasFields, isPresent } from "@/lib/shared/json";
import { Fields } from "@/components/flow/diagnostics/value/ValueFields";
import { ALERT } from "@/components/flow/diagnostics/value/DetailRecords";

/**
 * A Step method failure. A Worker error carries its own type, detail, gRPC status and stack trace;
 * the server's detail is shown only when there is no Worker detail.
 */
export function FailureDetails({
  value,
  stackInitiallyExpanded = false,
  showAttempt = true,
}: {
  value: unknown;
  stackInitiallyExpanded?: boolean;
  showAttempt?: boolean;
}) {
  const failure = asRecord(value);
  if (!hasFields(failure)) return null;
  const details = asRecord(failure.details);
  const workerStatus = isPresent(details.originalWorkerErrorStatus)
    ? grpcStatusLabel(details.originalWorkerErrorStatus)
    : undefined;
  const workerDetail = isPresent(details.originalWorkerErrorDetail) ? details.originalWorkerErrorDetail : undefined;
  const serverDetail = workerDetail === undefined ? details.detail : undefined;
  return (
    <div className={ALERT}>
      <Fields
        compact
        stacked
        values={[
          ["Attempt", showAttempt ? failure.attempt : undefined],
          ["Error type", backendErrorLabel(failure.backendError)],
          ["Detail", serverDetail],
          ["Worker error type", details.originalWorkerErrorType],
          ["Worker error detail", workerDetail],
          ["Worker gRPC status", workerStatus],
        ]}
      />
      {isPresent(details.originalWorkerErrorStackTrace) && (
        <details className="mt-1" open={stackInitiallyExpanded}>
          <summary className="cursor-pointer font-bold">Stack trace</summary>
          <pre className="mt-1.5 max-h-70 overflow-auto rounded-md border border-line-faint bg-raised p-2 font-mono text-[10px]/[1.5] whitespace-pre text-ink-dim">
            {String(details.originalWorkerErrorStackTrace)}
          </pre>
        </details>
      )}
    </div>
  );
}
