// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { storedValueJSONReplacer } from "@/lib/api/flow/flow-blobs";
import { cx } from "@/components/ui/cx";
import { WELL } from "./DetailRecords";
import { StructuredValue } from "./StructuredValue";
import { DetailsRawTabs } from "./ValueFields";

const openByKey = new Map<string, boolean>();
const viewByKey = new Map<string, "details" | "raw">();

export function JsonView({
  value,
  label = "Details",
  initiallyOpen = false,
  persistKey,
  forceOpen = false,
  parentFlowId = "",
  stepExecutionId = "",
  persistenceKind,
}: {
  value: unknown;
  label?: string;
  initiallyOpen?: boolean;
  persistKey?: string;
  /** Held open by the host's "Expand all"; releasing it collapses the view. */
  forceOpen?: boolean;
  parentFlowId?: string;
  stepExecutionId?: string;
  persistenceKind?: "attributes" | "channels";
}) {
  const storageKey = persistKey ?? label;
  const [open, setOpen] = useState(() => openByKey.get(storageKey) ?? initiallyOpen);
  const [view, setView] = useState<"details" | "raw">(() => viewByKey.get(storageKey) ?? "details");

  const [observedForceOpen, setObservedForceOpen] = useState(forceOpen);
  if (observedForceOpen !== forceOpen) {
    setObservedForceOpen(forceOpen);
    setOpen(forceOpen);
  }
  useEffect(() => {
    openByKey.set(storageKey, forceOpen || open);
  }, [forceOpen, open, storageKey]);

  const effectiveOpen = forceOpen || open;

  return (
    <div className="mt-2 flex w-full flex-col overflow-hidden rounded-lg border border-line-soft">
      <button
        type="button"
        className="flex w-full items-center gap-2 bg-raised px-2.5 py-2 text-left text-[11px] font-bold text-ink-dim"
        disabled={forceOpen}
        onClick={() => {
          setOpen((current) => {
            const next = !current;
            openByKey.set(storageKey, next);
            return next;
          });
        }}
      >
        <span className="text-base text-ink-faint">{effectiveOpen ? "−" : "+"}</span>
        {label}
      </button>
      {effectiveOpen && (
        <div className="flex flex-col gap-2 border-t border-line-faint bg-raised p-2.5 text-ink-dim">
          <DetailsRawTabs
            label={`${label} view`}
            view={view}
            onView={(next) => {
              setView(next);
              viewByKey.set(storageKey, next);
            }}
          />
          {view === "details" ? (
            <StructuredValue
              value={value}
              parentFlowId={parentFlowId}
              stepExecutionId={stepExecutionId}
              persistenceKind={persistenceKind}
            />
          ) : (
            <pre className={cx(WELL, "max-h-[420px]")}>{JSON.stringify(value, storedValueJSONReplacer, 2)}</pre>
          )}
        </div>
      )}
    </div>
  );
}
