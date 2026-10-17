// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { Badge } from "@/components/ui/badge/Badge";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import type { BuiltExample, ProcessTemplate } from "./template-catalog";

/**
 * A built example: its category, the apps it works with, who decides and what it does, with one action,
 * Open example, which creates the Project from the example as it is built and opens it. The closest example
 * of a ranking is marked.
 */
export function BuiltExampleCard({
  example,
  closest,
  disabled,
  onOpen,
}: {
  example: BuiltExample;
  closest: boolean;
  disabled: boolean;
  onOpen: (example: BuiltExample) => void;
}) {
  return (
    <article
      aria-label={example.title}
      data-testid="start-example"
      data-closest={closest ? "true" : undefined}
      className={cx(
        "flex w-full flex-col gap-1 rounded-xl border bg-panel p-3.5",
        closest ? "border-accent shadow-(--shadow-lift)" : "border-line-soft",
      )}
    >
      <span className="flex items-center justify-between gap-2 text-caption text-ink-faint">
        {example.category}
        {closest ? <Badge tone="accent">Closest</Badge> : <Badge tone="pass">Built and tested</Badge>}
      </span>
      <h3 className="text-reading font-semibold text-ink">{example.title}</h3>
      <span className="text-caption text-ink-dim">{example.apps.join(" · ")}</span>
      <p className="line-clamp-3 text-caption text-ink-faint">{example.description}</p>
      <span className="mt-1 text-caption text-ink-faint">{example.decider} decides</span>
      <Button
        variant={closest ? "primary" : "secondary"}
        disabled={disabled}
        className="mt-2.5 self-start"
        onClick={() => onOpen(example)}
      >
        Open example
      </Button>
    </article>
  );
}

/**
 * A placeholder, which is only a description: picking it fills the composer with its text to read and
 * change before building. It stays quieter than a built example.
 */
export function PlaceholderCard({
  template,
  picked,
  disabled,
  onPick,
}: {
  template: ProcessTemplate;
  picked: boolean;
  disabled: boolean;
  onPick: (template: ProcessTemplate) => void;
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      aria-pressed={picked}
      onClick={() => onPick(template)}
      data-testid="start-template"
      className={cx(
        "flex w-full flex-col gap-1 rounded-xl border border-dashed bg-transparent p-3.5 text-left focus-visible:shadow-(--focus-ring) focus-visible:outline-none enabled:hover:bg-panel disabled:opacity-50",
        picked ? "border-accent" : "border-line-soft enabled:hover:border-line",
      )}
    >
      <span className="text-caption text-ink-faint">{template.category}</span>
      <span className="text-reading font-medium text-ink-dim">{template.title}</span>
      <span className="text-caption text-ink-faint">{template.apps.join(" · ")}</span>
      <span className="mt-1.5 text-caption text-accent">Fill in this description</span>
    </button>
  );
}
