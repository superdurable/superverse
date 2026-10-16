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
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { isRecord } from "@/lib/shared/json";

/** What the agent asks to do, read from its arguments: the description and command it sent, when it did. */
function requestOf(argumentsJson: string): { description: string; command: string } {
  let value: unknown = null;
  try {
    value = JSON.parse(argumentsJson);
  } catch {
    // Arguments that are not JSON show only in the details.
  }
  const text = (key: string) => (isRecord(value) && typeof value[key] === "string" ? value[key] : "");
  return { description: text("description"), command: text("command") };
}

/**
 * A tool call waiting for approval, in plain words: what the agent wants to do and the command it
 * would run, with the tool and the opaque JSON the Agent sent one click away. Amber, because an
 * approval is the one thing blocking on a person.
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
  const { description, command } = requestOf(argumentsJson);
  const tool = toolName.replaceAll(/_+/g, " ").trim();
  return (
    <section className="rounded-lg border border-busy-rule bg-busy-subtle p-3" aria-label="Approval required">
      <Eyebrow>Approval required</Eyebrow>
      <p className="mt-2 text-body font-medium text-ink">{description || `Use the ${tool} tool`}</p>
      {command && (
        <pre className="mt-1.5 max-h-[min(160px,30dvh)] overflow-auto rounded-lg border border-line-soft bg-well px-3 py-2 font-mono text-caption wrap-anywhere whitespace-pre-wrap text-ink">
          {command}
        </pre>
      )}
      <MoreDetails surface="chat.approval" className="mt-2">
        <p className="text-caption text-ink-dim">Tool: {toolName}</p>
        <pre className="mt-1 max-h-[min(240px,40dvh)] overflow-auto rounded-lg border border-line-soft bg-well px-3 py-2.5 font-mono text-caption wrap-anywhere whitespace-pre-wrap text-ink">
          {argumentsJson}
        </pre>
      </MoreDetails>
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
