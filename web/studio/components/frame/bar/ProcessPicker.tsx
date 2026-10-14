// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useRef } from "react";
import { processName } from "@/lib/shared/format";
import { Glyph } from "@/components/ui/Glyph";
import { POPOVER_ITEM, PopoverCard, usePopover } from "@/components/ui/Popover";

/** One Process the picker offers: its Flow Type and its name for people. */
export interface PickerProcess {
  flowType: string;
  name: string;
  /** Changed since Live, on Preview. */
  changed?: boolean;
  /** In the agent's Flow design, which the Flow tab draws instead. */
  designed?: boolean;
}

/**
 * The picker at the head of the tabs: it narrows every surface to one Process, or shows all of them.
 * Pages of a Process join it once the AppManifest maps them to Flow Types. Its pill wears the
 * environment's colour: grey on Preview, green on Live.
 */
export function ProcessPicker({
  processes,
  current,
  hrefOf,
}: {
  processes: readonly PickerProcess[];
  current: string | null;
  hrefOf: (process: string | null) => string;
}) {
  const anchor = useRef<HTMLDivElement>(null);
  const popover = usePopover(anchor);
  const picked = processes.find((process) => process.flowType === current);
  const label = picked?.name ?? (current ? processName(current) : "All processes");
  return (
    <div ref={anchor} className="relative flex shrink-0 items-center">
      <button
        type="button"
        aria-expanded={popover.open}
        aria-haspopup="dialog"
        aria-label={`Process: ${label}`}
        title={picked ? picked.flowType : "Narrow every tab to one process"}
        onClick={popover.toggle}
        className="my-2.25 mr-1.5 inline-flex h-7.5 max-w-80 items-center gap-1.5 truncate rounded-[7px] bg-env-subtle px-2.5 text-[13px] font-semibold whitespace-nowrap text-ink shadow-[inset_0_0_0_1px_color-mix(in_srgb,var(--color-env)_30%,transparent)] group-data-[narrow=true]/bar:max-w-45 group-data-[tight=true]/bar:max-w-28 hover:bg-[color-mix(in_srgb,var(--color-env-subtle)_70%,var(--color-env)_12%)] focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
      >
        <span className="min-w-0 truncate">{label}</span>
        <Glyph name="chevronDown" className="size-3.5" />
      </button>
      {popover.open && (
        <PopoverCard anchorRef={anchor} label="Processes" className="w-80">
          <Link
            href={hrefOf(null)}
            onClick={popover.close}
            aria-current={current === null ? "page" : undefined}
            className={POPOVER_ITEM}
          >
            All processes
          </Link>
          {processes.length > 0 && <hr className="mx-0.5 my-1 border-line-faint" />}
          {processes.map((process) => (
            <Link
              key={process.flowType}
              href={hrefOf(process.flowType)}
              onClick={popover.close}
              aria-current={process.flowType === current ? "page" : undefined}
              className={POPOVER_ITEM}
            >
              <span className="flex min-w-0 flex-col items-start gap-px">
                <b className="font-semibold">{process.name}</b>
                <small className="font-mono text-[11px] text-ink-faint">{process.flowType}</small>
              </span>
              {process.designed ? (
                <span className="ml-auto shrink-0 rounded-full bg-accent-subtle px-1.5 text-[10.5px] text-accent">
                  design
                </span>
              ) : (
                process.changed && (
                  <span className="ml-auto shrink-0 rounded-full bg-busy-subtle px-1.5 text-[10.5px] text-busy">
                    changed
                  </span>
                )
              )}
            </Link>
          ))}
          {processes.length === 0 && (
            <p className="px-2.25 py-1.5 text-[12.5px] text-ink-faint">No processes in this version yet.</p>
          )}
        </PopoverCard>
      )}
    </div>
  );
}
