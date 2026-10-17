// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useMemo, useState } from "react";
import type { ExampleCategory } from "@/lib/api/example";
import { cx } from "@/components/ui/cx";
import { BuiltExampleCard } from "./ExampleCard";
import { appsOf, browsedExamples, CATEGORY_ORDER, type ExampleIndex } from "./example-match";
import type { BuiltExample } from "./template-catalog";

const FIELD =
  "rounded-lg border border-line bg-panel px-3 py-2 text-body text-ink placeholder:text-ink-faint focus:border-accent focus:shadow-(--focus-ring) focus:outline-none";

/**
 * Every built example, to search by words and narrow by category and app: the closest first for a search,
 * otherwise by category and title. Each opens like a card of the grid.
 */
export function ExampleBrowser({
  index,
  disabled,
  onOpen,
}: {
  index: ExampleIndex;
  disabled: boolean;
  onOpen: (example: BuiltExample) => void;
}) {
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<ExampleCategory | null>(null);
  const [app, setApp] = useState<string | null>(null);
  const apps = useMemo(() => appsOf(index.examples), [index]);
  const shown = useMemo(() => browsedExamples(index, { query, category, app }), [index, query, category, app]);
  return (
    <div className="mt-3 grid gap-3 rounded-xl border border-line-soft bg-well p-3.5">
      <div className="flex flex-wrap gap-2">
        <input
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          aria-label="Search examples"
          placeholder="Search examples"
          className={cx(FIELD, "min-w-0 flex-1")}
        />
        <select
          aria-label="Category"
          value={category ?? ""}
          onChange={(event) => setCategory(CATEGORY_ORDER.find((value) => value === event.target.value) ?? null)}
          className={FIELD}
        >
          <option value="">Every category</option>
          {CATEGORY_ORDER.map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
        <select
          aria-label="App"
          value={app ?? ""}
          onChange={(event) => setApp(event.target.value || null)}
          className={FIELD}
        >
          <option value="">Every app</option>
          {apps.map((value) => (
            <option key={value} value={value}>
              {value}
            </option>
          ))}
        </select>
      </div>
      <p className="text-caption text-ink-faint" aria-live="polite">
        {shown.length === 1 ? "1 example" : `${String(shown.length)} examples`}
      </p>
      {shown.length > 0 && (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-2.5">
          {shown.map((example) => (
            <li key={example.id} className="flex">
              <BuiltExampleCard example={example} closest={false} disabled={disabled} onOpen={onOpen} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
