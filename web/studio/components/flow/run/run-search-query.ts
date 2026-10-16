// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useState } from "react";
import type { FlowV2Definition, V2ValueType } from "@/lib/api/flow/flow-definition";
import type { SearchFilter } from "@/lib/api/flow/flow-run";
import { SEARCH_STATUS_VALUES } from "@/lib/api/flow/flow-status";
import { parseTypedValue } from "@/components/flow/run/detail/TypedValueInput";

/**
 * Two controls in front, three behind a disclosure. Not a query builder.
 *
 * Every one compiles to a filter Dex can actually index: there is no field here that the Flow did
 * not declare, and no operator offered that the index type will reject.
 */
export interface RunQuery {
  /** An execution status label, or '' for any. */
  status: string;
  /** A relative window, or '' for any. Relative because nobody remembers a timestamp. */
  since: SinceWindow;
  /** An exact Flow ID. Its own control because looking one up is not the same as narrowing a list. */
  flowId: string;
  /** One declared Indexed Attribute, or '' for none. */
  attributeKey: string;
  attributeOperator: RunOperator;
  attributeValue: string;
}

/**
 * The four comparisons worth offering, each against exactly one value. `in` is deliberately absent:
 * it needs a way to enter several values, and a second value control the moment anybody wants two
 * ranges. Add it when a real query needs it.
 */
export type RunOperator = "eq" | "contains" | "gte" | "lte";

export type SinceWindow = "" | "1h" | "24h" | "7d" | "30d";

export const SINCE_WINDOWS: { value: SinceWindow; label: string; hours: number }[] = [
  { value: "", label: "Any time", hours: 0 },
  { value: "1h", label: "Last hour", hours: 1 },
  { value: "24h", label: "Last 24 hours", hours: 24 },
  { value: "7d", label: "Last 7 days", hours: 24 * 7 },
  { value: "30d", label: "Last 30 days", hours: 24 * 30 },
];

export const EMPTY_RUN_QUERY: RunQuery = {
  status: "",
  since: "",
  flowId: "",
  attributeKey: "",
  attributeOperator: "eq",
  attributeValue: "",
};

const OPERATOR_LABEL: Record<RunOperator, string> = {
  eq: "equals",
  contains: "contains",
  gte: "at least",
  lte: "at most",
};

/** Every status shown in the run list; continued-as-new executions belong to their successor. */
export function statusOptions(): string[] {
  return Object.values(SEARCH_STATUS_VALUES).filter((label) => label !== "Unspecified" && label !== "Continued as new");
}

/**
 * What each index type can actually be asked.
 *
 * `contains` is rejected server-side unless the index is fulltext, and ordering makes no sense on a
 * keyword, so offering the whole set everywhere would hand the reader queries that only fail.
 */
export function operatorsFor(indexType: string): { value: RunOperator; label: string }[] {
  const ordered: RunOperator[] =
    indexType === "fulltext"
      ? ["contains", "eq"]
      : indexType === "int" || indexType === "double" || indexType === "datetime"
        ? ["eq", "gte", "lte"]
        : ["eq"];
  return ordered.map((value) => ({ value, label: OPERATOR_LABEL[value] }));
}

/** The comparison a reader most likely wants for this index type, used when they pick an attribute. */
export function defaultOperator(indexType: string): RunOperator {
  return indexType === "fulltext" ? "contains" : "eq";
}

/** What the search reads of a definition: the Attributes it indexes. All processes search none. */
export type SearchableDefinition = Pick<FlowV2Definition, "indexedAttributes">;

export function attributeByKey(definition: SearchableDefinition, key: string) {
  return definition.indexedAttributes.find((attribute) => attribute.attributeKey === key) ?? null;
}

function searchFilter(field: string, operator: string, text: string, valueType: V2ValueType): SearchFilter {
  return { field, operator, values: [parseTypedValue(text, valueType)] };
}

/** Compile the controls into the filters the search endpoint understands. */
function toSearchFilters(query: RunQuery, definition: SearchableDefinition, nowMs: number): SearchFilter[] {
  const filters: SearchFilter[] = [];

  if (query.status !== "") {
    filters.push(searchFilter("executionStatus", "eq", query.status, "string"));
  }

  const window = SINCE_WINDOWS.find((candidate) => candidate.value === query.since);
  if (window !== undefined && window.hours > 0) {
    const from = new Date(nowMs - window.hours * 3_600_000);
    filters.push(searchFilter("startTime", "gte", from.toISOString(), "datetime"));
  }

  const flowId = query.flowId.trim();
  if (flowId !== "") {
    filters.push(searchFilter("flowId", "eq", flowId, "string"));
  }

  const value = query.attributeValue.trim();
  const attribute = attributeByKey(definition, query.attributeKey);
  if (attribute !== null && value !== "") {
    const legal = operatorsFor(attribute.indexType).map((operator) => operator.value);
    const operator = legal.includes(query.attributeOperator)
      ? query.attributeOperator
      : defaultOperator(attribute.indexType);
    filters.push(searchFilter(attribute.attributeKey, operator, value, attribute.valueType));
  }

  return filters;
}

/** Whether anything behind the disclosure is set, so a closed panel cannot hide an active filter. */
export function hasAdvancedQuery(query: RunQuery): boolean {
  return query.flowId.trim() !== "" || (query.attributeKey !== "" && query.attributeValue.trim() !== "");
}

/**
 * Holds the four controls and compiles them to search filters on submit, so a half-typed keyword
 * is not a query and the filters the list was searched with stay stable while the next is edited.
 */
export function useRunQuery(definition: SearchableDefinition | undefined, initial: RunQuery = EMPTY_RUN_QUERY) {
  // The view's own scope, which Clear returns to: the Inbox's open runs, or every run.
  const [scope] = useState(initial);
  const [query, setQuery] = useState<RunQuery>(initial);
  const [applied, setApplied] = useState<{ query: RunQuery; filters: SearchFilter[] }>(() => ({
    query: initial,
    filters: definition === undefined ? [] : toSearchFilters(initial, definition, Date.now()),
  }));

  const apply = useCallback(
    (next: RunQuery) => {
      if (definition === undefined) return;
      setQuery(next);
      setApplied({ query: next, filters: toSearchFilters(next, definition, Date.now()) });
    },
    [definition],
  );

  return {
    query,
    setQuery,
    appliedFilters: applied.filters,
    /** How many controls the list was searched with differ from the view's own scope. */
    activeFilters: changedControls(applied.query, scope),
    submit: useCallback(() => apply(query), [apply, query]),
    clear: useCallback(() => apply(scope), [apply, scope]),
  };
}

/** The controls of `query` that differ from `scope`; an attribute counts once, and only with a value. */
function changedControls(query: RunQuery, scope: RunQuery): number {
  const attribute = (of: RunQuery) =>
    of.attributeValue.trim() === "" ? "" : `${of.attributeKey}:${of.attributeValue}`;
  return [
    query.status !== scope.status,
    query.since !== scope.since,
    query.flowId.trim() !== scope.flowId.trim(),
    attribute(query) !== attribute(scope),
  ].filter(Boolean).length;
}
