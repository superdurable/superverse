// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { JSX } from "react";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import type { DesignedFlow, DesignedStep } from "./design-graph";

const HEADING = cx("mt-2.5 mb-1 text-xs font-semibold tracking-wider text-ink-faint uppercase first:mt-0");
const PROSE = cx("text-sm/[1.45] text-ink-dim");
const NOTE = cx("text-[13px]/normal text-ink-faint");

/** What a designed Step waits for, in words. */
function waitWords(step: DesignedStep, flow: DesignedFlow): string {
  const { kind, name } = step.waitsFor ?? { kind: "KIND_NONE", name: "" };
  if (kind === "KIND_ACTION") {
    const action = flow.actions.find((item) => item.rpc === name);
    return `Waits until someone chooses ${action ? `“${action.label}” (${name})` : name}.`;
  }
  if (kind === "KIND_CHANNEL") return `Waits for a message on ${name}.`;
  if (kind === "KIND_TIMER") return `Waits until ${name}.`;
  return "Runs at once, without waiting.";
}

/** One way out of a designed Step: where it leads and when. */
function wayOut(next: DesignedStep["next"][number]): string {
  const to =
    next.end === "END_COMPLETE"
      ? "the process completes"
      : next.end === "END_FAIL"
        ? "the process fails"
        : `goes to ${next.toStep}`;
  const when = next.when ? `${next.when}: ` : "";
  return next.isFailureRoute ? `If it fails, ${when}${to} (failure handling)` : `${when}${to}`;
}

/** A designed Step: its explanation, its wait, its ways out and the card's features it serves. */
export function DesignStepPanel({
  step,
  flow,
  features,
  onClose,
}: {
  step: DesignedStep;
  flow: DesignedFlow;
  features: readonly { id: string; text: string }[];
  onClose: () => void;
}): JSX.Element {
  const served = features.filter((feature) => step.features.includes(feature.id));
  return (
    // Opaque and over the canvas, like the definition panel, so opening it never changes the zoom.
    <aside
      className="absolute inset-y-0 right-0 z-6 flex w-[var(--v2-panel-w,290px)] min-w-0 animate-panel-in flex-col overflow-hidden border-l border-line-faint bg-bg motion-reduce:animate-none"
      aria-label={`Design of ${step.stepType}`}
    >
      <header className="flex items-start justify-between gap-2 border-b border-line-faint px-3.5 pt-3 pb-2.5">
        <div>
          <h2 className="m-0 text-[17px] font-semibold">{step.stepType}</h2>
          <p className="mt-0.5 text-[12.5px] text-ink-faint">Designed Step{step.group ? ` · ${step.group}` : ""}</p>
        </div>
        <Button className="flex-none" size="sm" onClick={onClose} aria-label="Close panel">
          ✕
        </Button>
      </header>
      <section className="min-h-0 flex-auto overflow-y-auto px-3.5 py-3" aria-label="Designed Step">
        <h3 className={HEADING}>Explanation</h3>
        <p className={step.explanation ? PROSE : NOTE}>{step.explanation || "The design gives no explanation."}</p>
        <h3 className={HEADING}>Waits for</h3>
        <p className={PROSE}>{waitWords(step, flow)}</p>
        <h3 className={HEADING}>Ways out</h3>
        <ul className="grid list-disc gap-1 pl-4.5 text-sm text-ink-dim">
          {step.next.map((next, index) => (
            <li className="wrap-anywhere" key={index}>
              {wayOut(next)}
            </li>
          ))}
        </ul>
        <h3 className={HEADING}>Serves</h3>
        {served.length === 0 ? (
          <p className={NOTE}>No feature of the card names this Step.</p>
        ) : (
          <ul className="grid gap-1 text-sm text-ink-dim">
            {served.map((feature) => (
              <li className="flex gap-2" key={feature.id}>
                <strong className="shrink-0 font-semibold text-ink">{feature.id}</strong>
                <span className="wrap-anywhere">{feature.text}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </aside>
  );
}
