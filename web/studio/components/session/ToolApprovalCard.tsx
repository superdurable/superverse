// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { Button } from "@/components/ui/Button";
import { Eyebrow } from "@/components/ui/Eyebrow";

/**
 * A tool call waiting for approval, with its arguments as the opaque JSON the Agent sent. Amber,
 * because an approval is the one thing blocking on a person.
 */
export function ToolApprovalCard({
  toolName,
  argumentsJson,
  disabled = false,
  isSubmitting = false,
  onApprove,
  onReject,
}: {
  toolName: string;
  argumentsJson: string;
  disabled?: boolean;
  isSubmitting?: boolean;
  onApprove: () => void;
  onReject: () => void;
}) {
  const label = isSubmitting ? "Processing…" : null;
  return (
    <section className="rounded-lg border border-busy-rule bg-busy-subtle p-3" aria-label="Approval required">
      <Eyebrow>Approval required</Eyebrow>
      <p className="mt-2 mb-1.5 text-[13px] font-medium tracking-wide text-ink-dim">
        tool call: {toolName.replaceAll(/_+/g, " ").trim()}
      </p>
      <pre className="max-h-[min(240px,40dvh)] overflow-auto rounded-lg border border-line-soft bg-well px-3 py-2.5 font-mono text-xs/normal wrap-anywhere whitespace-pre-wrap text-ink">
        {argumentsJson}
      </pre>
      <div className="mt-3 flex flex-wrap gap-2">
        <Button variant="primary" disabled={disabled} onClick={onApprove}>
          {label ?? "Approve"}
        </Button>
        <Button variant="danger" disabled={disabled} onClick={onReject}>
          {label ?? "Reject"}
        </Button>
      </div>
    </section>
  );
}
