// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import { SEARCH_COPY } from "@/components/flow/run/detail/run-copy";
import {
  SINCE_WINDOWS,
  attributeByKey,
  defaultOperator,
  hasAdvancedQuery,
  operatorsFor,
  statusOptions,
  type RunQuery,
  type SearchableDefinition,
} from "./run-search-query";
import { TypedValueInput } from "@/components/flow/run/detail/TypedValueInput";
import { Button } from "@/components/ui/Button";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { cx } from "@/components/ui/cx";
import { FIELD, LABEL } from "@/components/flow/frame/workspace-styles";

/** The search controls are smaller than a case's, so the column keeps its width for the runs. */
const SEARCH_FIELD = cx(FIELD, "text-xs");
const SEARCH_LABEL = cx(LABEL, "mb-1 block");

/**
 * Status and time in front; attribute comparisons and an exact Flow ID behind a disclosure.
 *
 * Two controls answer most questions about a list of runs, and they are the two every Flow has
 * whether or not it declared anything indexable. What only some Flows can answer, and what needs a
 * reader to choose an operator, goes one click away rather than into the default view.
 */
export function RunSearch({
  definition,
  query,
  busy,
  onChange,
  onSubmit,
  onClear,
}: {
  definition: SearchableDefinition;
  query: RunQuery;
  busy: boolean;
  onChange: (query: RunQuery) => void;
  onSubmit: () => void;
  onClear: () => void;
}) {
  /**
   * The disclosure owns its own open state after the first render.
   *
   * Deriving `open` from the query collapsed the panel the moment somebody chose an Attribute,
   * because a chosen Attribute with no value yet is not an active filter.
   */
  const [advancedOpen, setAdvancedOpen] = useState(() => hasAdvancedQuery(query));
  const attributes = definition.indexedAttributes;
  const chosen = attributeByKey(definition, query.attributeKey);
  const operators = chosen === null ? [] : operatorsFor(chosen.indexType);
  const set = (patch: Partial<RunQuery>) => onChange({ ...query, ...patch });
  return (
    <form
      className="flex flex-col gap-1.5"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <div className="grid grid-cols-2 gap-1.5">
        <Dropdown
          label={SEARCH_COPY.statusLabel}
          value={query.status}
          onChange={(status) => set({ status })}
          items={[
            { value: "", label: SEARCH_COPY.anyStatus },
            ...statusOptions().map((status) => ({ value: status, label: status })),
          ]}
        />
        <Dropdown
          label={SEARCH_COPY.sinceLabel}
          value={query.since}
          onChange={(since) => set({ since })}
          items={SINCE_WINDOWS.map((window) => ({ value: window.value, label: window.label }))}
        />
      </div>

      <details
        className="group mt-px"
        open={advancedOpen}
        onToggle={(event) => setAdvancedOpen(event.currentTarget.open)}
      >
        <summary className="cursor-pointer list-none py-0.75 text-[11px] text-ink-faint before:mr-1.25 before:inline-block before:transition-transform before:content-['▸'] group-open:before:rotate-90 hover:text-ink [&::-webkit-details-marker]:hidden">
          {SEARCH_COPY.advanced}
        </summary>

        <label className="mt-1.5 block">
          <span className={SEARCH_LABEL}>{SEARCH_COPY.flowIdLabel}</span>
          <input
            className={cx(SEARCH_FIELD, "font-mono")}
            placeholder={SEARCH_COPY.flowIdPlaceholder}
            type="search"
            value={query.flowId}
            onChange={(event) => set({ flowId: event.target.value })}
          />
        </label>

        {attributes.length > 0 && (
          <div className="mt-1.5">
            <span className={SEARCH_LABEL}>{SEARCH_COPY.attributeLabel}</span>
            <Dropdown
              label={SEARCH_COPY.attributeLabel}
              value={query.attributeKey}
              onChange={(attributeKey) => {
                const next = attributeByKey(definition, attributeKey);
                set({
                  attributeKey,
                  attributeOperator: next === null ? "eq" : defaultOperator(next.indexType),
                  attributeValue: "",
                });
              }}
              items={[
                { value: "", label: SEARCH_COPY.anyAttribute },
                ...attributes.map((attribute) => ({ value: attribute.attributeKey, label: attribute.description })),
              ]}
            />
            <div className="mt-1 grid grid-cols-[auto_minmax(0,1fr)] gap-1.5">
              <Dropdown
                label={SEARCH_COPY.operatorLabel}
                inline
                disabled={chosen === null}
                placeholder="="
                value={query.attributeOperator}
                onChange={(attributeOperator) => set({ attributeOperator })}
                items={operators}
              />
              <TypedValueInput
                className={SEARCH_FIELD}
                ariaLabel={SEARCH_COPY.attributeValueLabel}
                disabled={chosen === null}
                emptyLabel={SEARCH_COPY.anyValue}
                placeholder={chosen === null ? SEARCH_COPY.pickAttribute : SEARCH_COPY.value}
                valueType={chosen?.valueType}
                value={query.attributeValue}
                onChange={(value) => set({ attributeValue: value })}
              />
            </div>
          </div>
        )}
      </details>

      <div className="mt-0.5 mb-1.5 flex gap-1.5">
        <Button variant="primary" size="sm" disabled={busy} type="submit">
          {busy ? SEARCH_COPY.searching : SEARCH_COPY.search}
        </Button>
        <Button size="sm" onClick={onClear}>
          {SEARCH_COPY.clear}
        </Button>
      </div>
    </form>
  );
}
