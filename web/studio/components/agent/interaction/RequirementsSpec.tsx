// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import { cx } from "@/components/ui/cx";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import type { Requirements } from "@/lib/session/agent-types";

const PART_TITLE = "text-[11px] font-semibold tracking-wider text-ink-faint uppercase";

function Part({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid gap-1.5" aria-label={title}>
      <h3 className={PART_TITLE}>{title}</h3>
      {children}
    </section>
  );
}

function BulletList({ items, empty }: { items: readonly string[]; empty: string }) {
  if (items.length === 0) return <p className="text-ink-faint">{empty}</p>;
  return (
    <ul className="grid list-disc gap-1 pl-5">
      {items.map((item) => (
        <li className="wrap-anywhere" key={item}>
          {item}
        </li>
      ))}
    </ul>
  );
}

/** The card's Flow design as text: its summary and, behind More details, the outline with one line per Flow in Go names. */
function DesignPart({ design }: { design: NonNullable<Requirements["design"]> }) {
  return (
    <Part title="Design">
      <p className="wrap-anywhere">{design.summary}</p>
      <MoreDetails surface="brief.outline" hint="outline">
        <ul className="grid gap-1 font-mono text-xs">
          {design.outline.split("\n").map((line) => (
            <li className="wrap-anywhere" key={line}>
              {line}
            </li>
          ))}
        </ul>
      </MoreDetails>
    </Part>
  );
}

/**
 * The parts of a set of requirements: its features, its Flow design when it has one, connections, what the user
 * prepares and acceptance.
 */
export function RequirementsSpec({ spec }: { spec: Requirements }) {
  return (
    <div className="grid gap-3 text-[13px] text-ink-dim">
      <p className="wrap-anywhere">{spec.summary}</p>
      <Part title="Features">
        <ol className="grid gap-1.5">
          {spec.features.map((feature) => (
            <li className="flex gap-2" key={feature.id}>
              <strong className="shrink-0 font-semibold text-ink">{feature.id}</strong>
              <span className="wrap-anywhere">{feature.text}</span>
            </li>
          ))}
        </ol>
      </Part>
      {spec.design && <DesignPart design={spec.design} />}
      <Part title="Connections">
        {spec.connections.length === 0 ? (
          <p className="text-ink-faint">No connections to other systems.</p>
        ) : (
          <ul className="grid gap-1.5">
            {spec.connections.map((connection) => (
              <li
                className="grid gap-0.5 rounded-lg border border-line-soft bg-panel px-3 py-2"
                key={`${connection.connector}:${connection.need}`}
              >
                <span className="flex flex-wrap items-center gap-2">
                  <strong className="font-semibold text-ink">{connection.connector}</strong>
                  <span
                    className={cx(
                      "rounded-md border px-1.5 text-[11px]",
                      connection.status === "new"
                        ? "border-busy-rule bg-busy-subtle text-busy"
                        : "border-pass-rule bg-pass-subtle text-pass",
                    )}
                  >
                    {connection.status === "new" ? "New" : "Existing"}
                  </span>
                </span>
                <span className="wrap-anywhere">{connection.need}</span>
                <span className="text-xs wrap-anywhere text-ink-faint">
                  Operations: {connection.operations.join(", ")}
                </span>
                {connection.userSetup !== "" && (
                  <span className="text-xs wrap-anywhere">You set up: {connection.userSetup}</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </Part>
      <Part title="You need to prepare">
        <BulletList items={spec.userSetup} empty="Nothing to prepare." />
      </Part>
      <Part title="Acceptance">
        <BulletList items={spec.acceptance} empty="No acceptance steps." />
      </Part>
    </div>
  );
}
