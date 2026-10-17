// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useMemo, useState } from "react";
import { Button } from "@/components/ui/Button";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { ExampleBrowser } from "./ExampleBrowser";
import { BuiltExampleCard, PlaceholderCard } from "./ExampleCard";
import { closestExamples, describesProcess, indexExamples, starterTemplates } from "./example-match";
import { PROCESS_TEMPLATES, type BuiltExample, type ProcessTemplate } from "./template-catalog";
import { useCardMotion } from "./use-card-motion";
import { useSettledText } from "./use-settled-text";

/**
 * The start page's starting points under the composer. Before the person types, a few of each category: the
 * built examples, then the placeholders, which are quieter. While they type, the built examples closest to
 * the description, best first and marked, re-ranked at word boundaries or after a short pause, with the cards
 * moving to their new places. Open example creates the Project from a built example as it is and opens it;
 * the description is not sent, it waits in the Project's chat. A placeholder fills the composer. Browse all
 * examples shows the whole catalog in the grid's place, to search and filter.
 */
export function ExampleGrid({
  description,
  examples,
  showPlaceholders,
  picked,
  disabled,
  onOpen,
  onPick,
}: {
  /** What the person typed, which ranks the examples; nothing of it is sent from here. */
  description: string;
  examples: readonly BuiltExample[];
  /** The placeholders fill the composer, which the mock model's scenario composer replaces. */
  showPlaceholders: boolean;
  /** The placeholder whose description the composer holds, if any. */
  picked: string | null;
  disabled: boolean;
  onOpen: (example: BuiltExample) => void;
  onPick: (template: ProcessTemplate) => void;
}) {
  const [browsing, setBrowsing] = useState(false);
  const index = useMemo(() => indexExamples(examples), [examples]);
  const settled = useSettledText(description);
  const ranking = describesProcess(settled);
  const shown: readonly ProcessTemplate[] = useMemo(
    () =>
      ranking ? closestExamples(index, settled) : starterTemplates(examples, showPlaceholders ? PROCESS_TEMPLATES : []),
    [examples, index, ranking, settled, showPlaceholders],
  );
  const cardRef = useCardMotion(shown.map((template) => `${template.kind}:${template.id}`).join(","));
  if (examples.length === 0 && !showPlaceholders) return null;
  return (
    <section aria-labelledby="start-examples" className="mt-8">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <Eyebrow id="start-examples">
          {browsing ? "All examples" : ranking ? "Closest to your description" : "Start from an example"}
        </Eyebrow>
        {examples.length > 0 && (
          <Button size="sm" aria-expanded={browsing} onClick={() => setBrowsing(!browsing)}>
            {browsing ? "Hide all examples" : "Browse all examples"}
          </Button>
        )}
      </div>
      <p className="mt-1 text-caption text-ink-faint">
        Open example creates the project from the built example as it is. What you typed waits in its chat, unsent, for
        you to change it.
      </p>
      {browsing && <ExampleBrowser index={index} disabled={disabled} onOpen={onOpen} />}
      {!browsing && ranking && shown.length === 0 && (
        <p className="mt-3 text-body text-ink-dim">No built example is close to your description yet.</p>
      )}
      {!browsing && (
        <ul className="relative mt-3 grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-2.5">
          {shown.map((template, position) => (
            <li
              key={`${template.kind}:${template.id}`}
              ref={cardRef(`${template.kind}:${template.id}`)}
              className="flex"
            >
              {template.kind === "example" ? (
                <BuiltExampleCard
                  example={template}
                  closest={ranking && position === 0}
                  disabled={disabled}
                  onOpen={onOpen}
                />
              ) : (
                <PlaceholderCard
                  template={template}
                  picked={template.id === picked}
                  disabled={disabled}
                  onPick={onPick}
                />
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
