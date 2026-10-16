// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "@/components/ui/cx";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { PROCESS_TEMPLATES, type ProcessTemplate } from "./template-catalog";

/**
 * The start page's templates as cards: each names its apps and who decides. Picking one puts its
 * description in the composer to read and change; it never sends anything.
 */
export function TemplateGallery({
  picked,
  disabled,
  onPick,
}: {
  /** The template whose description the composer holds, if any. */
  picked: string | null;
  disabled: boolean;
  onPick: (template: ProcessTemplate) => void;
}) {
  return (
    <section aria-labelledby="start-templates" className="mt-10">
      <Eyebrow id="start-templates">Or start from an example</Eyebrow>
      <ul className="mt-3 grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-2.5">
        {PROCESS_TEMPLATES.map((template) => {
          const selected = template.id === picked;
          return (
            <li key={template.id} className="flex">
              <button
                type="button"
                disabled={disabled}
                aria-pressed={selected}
                onClick={() => onPick(template)}
                className={cx(
                  "flex w-full flex-col gap-1 rounded-xl border bg-panel p-3.5 text-left focus-visible:shadow-(--focus-ring) focus-visible:outline-none enabled:hover:shadow-(--shadow-lift) disabled:opacity-50",
                  selected ? "border-accent" : "border-line-soft enabled:hover:border-line",
                )}
                data-testid="start-template"
              >
                <span className="text-caption text-ink-faint">{template.category}</span>
                <span className="text-reading font-semibold text-ink">{template.title}</span>
                <span className="text-caption text-ink-dim">{template.apps.join(" · ")}</span>
                <span className="mt-1.5 text-caption text-ink-faint">{template.decider} decides</span>
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
