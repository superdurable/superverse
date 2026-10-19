// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { SearchFilter } from "@/lib/api/flow/flow-run";
import type { FlowSearchResult, FlowSearchRow } from "@/lib/api/flow/flow-types";
import { RUNNING_FLOW_STATUS_CODE, SEARCH_STATUS_VALUES } from "@/lib/api/flow/flow-status";

/** The four groups of a run list, in the order a person reads them. */
export type RunGroupKey = "person" | "progress" | "failed" | "done";

export const RUN_GROUP_KEYS: readonly RunGroupKey[] = ["person", "progress", "failed", "done"];

const GROUP_LABEL: Record<RunGroupKey, string> = {
  person: "Need a person",
  progress: "In progress",
  failed: "Failed",
  done: "Done",
};

/** Failed and Timed out: a run that timed out failed too, while one a person stopped or canceled is done. */
const FAILED_STATUS_CODES = [3, 4] as const;
const FAILED_STATUSES = FAILED_STATUS_CODES.map((code) => SEARCH_STATUS_VALUES[code]);
const COMPLETED_STATUS = SEARCH_STATUS_VALUES[2];

export interface RunGroup {
  key: RunGroupKey;
  label: string;
  flows: FlowSearchRow[];
}

/**
 * Which runs need a person: every running one when the list is the Inbox's work queue, the Flow IDs
 * the health searches found otherwise, and none while that is unknown.
 */
export type WaitingRuns = ReadonlySet<string> | "all" | null;

function groupOf(flow: FlowSearchRow, waiting: WaitingRuns): RunGroupKey {
  if (flow.flowStatusCode === RUNNING_FLOW_STATUS_CODE)
    return waiting === "all" || waiting?.has(flow.flowId) ? "person" : "progress";
  return (FAILED_STATUS_CODES as readonly number[]).includes(flow.flowStatusCode) ? "failed" : "done";
}

/**
 * The runs by group, in the server's own newest-first order within each; empty groups are left out.
 *
 * A stable partition, so the order is never invented: /api/v2/search already sorts by start time
 * descending, and status is read from the run rather than inferred.
 */
export function groupRuns(flows: readonly FlowSearchRow[], waiting: WaitingRuns): RunGroup[] {
  return RUN_GROUP_KEYS.map((key) => ({
    key,
    label: GROUP_LABEL[key],
    flows: flows.filter((flow) => groupOf(flow, waiting) === key),
  })).filter((group) => group.flows.length > 0);
}

/** One page per Flow type and group, as the list reads; a full page counts as "50+". */
export const RUN_PAGE_SIZE = 50;

interface GroupSearch {
  workQueueOnly: boolean;
  filters: SearchFilter[];
}

const statusIn = (statuses: readonly string[]): SearchFilter => ({
  field: "executionStatus",
  operator: "in",
  values: [...statuses],
});

/**
 * The one search each group adds to a read, narrowed by the list's own filters so the counts describe
 * the list. "In progress" reads every running run and leaves out those that need a person. A group the
 * list's status filter already rules out reads nothing: it is empty.
 */
function groupSearches(applied: readonly SearchFilter[], midnight: Date): Record<RunGroupKey, GroupSearch | null> {
  const asked = applied.find((filter) => filter.field === "executionStatus")?.values ?? null;
  const search = (statuses: readonly string[], workQueueOnly: boolean, ...extra: SearchFilter[]) =>
    asked !== null && !statuses.some((status) => asked.includes(status))
      ? null
      : { workQueueOnly, filters: [...applied, statusIn(statuses), ...extra] };
  const running = [SEARCH_STATUS_VALUES[RUNNING_FLOW_STATUS_CODE]];
  return {
    person: search(running, true),
    progress: search(running, false),
    failed: search(FAILED_STATUSES, false),
    done: search([COMPLETED_STATUS], false, {
      field: "closeTime",
      operator: "gte",
      values: [midnight.toISOString()],
    }),
  };
}

/** A group's page for one Flow type: read, ruled out by the list's filters, or not readable just now. */
type GroupPage = FlowSearchResult | "ruled-out" | "unknown";
export type GroupPages = Record<RunGroupKey, GroupPage>;

/** Reads every group's page for one Flow type; a failed group is unknown and reported to the caller. */
export async function readGroupPages(
  read: (search: GroupSearch) => Promise<FlowSearchResult>,
  applied: readonly SearchFilter[],
  onFailure?: (error: unknown) => void,
): Promise<GroupPages> {
  const midnight = new Date();
  midnight.setHours(0, 0, 0, 0);
  const searches = groupSearches(applied, midnight);
  const pages = await Promise.all(
    RUN_GROUP_KEYS.map((key) => {
      const search = searches[key];
      return search === null
        ? "ruled-out"
        : read(search).catch((error): GroupPage => {
            onFailure?.(error);
            return "unknown";
          });
    }),
  );
  return Object.fromEntries(RUN_GROUP_KEYS.map((key, index) => [key, pages[index]])) as GroupPages;
}

/** What one group holds: its runs, newest first, and how many; `more` when there may be more than `count`. */
export interface GroupTally {
  flows: FlowSearchRow[];
  count: number;
  more: boolean;
}

/** The health of the runs a list shows: each group's tally, null where it is not known. */
export interface RunHealth {
  groups: Record<RunGroupKey, GroupTally | null>;
  /** The runs that need a person, which groups the list itself. */
  waiting: ReadonlySet<string> | null;
}

const EMPTY: GroupTally = { flows: [], count: 0, more: false };

function tally(page: GroupPage): GroupTally | null {
  if (page === "unknown") return null;
  if (page === "ruled-out") return EMPTY;
  const more = page.nextPageToken !== "" || page.flows.length >= RUN_PAGE_SIZE;
  return { flows: page.flows, count: page.flows.length, more };
}

/**
 * Every running run that does not need a person. When the person page is full, a running run older
 * than its oldest row may need one too, so only newer ones count and the count is a floor.
 */
function inProgress(running: GroupTally | null, person: GroupTally | null): GroupTally | null {
  if (running === null || person === null) return null;
  const waiting = new Set(person.flows.map((flow) => flow.flowId));
  const oldest = person.more ? Math.min(...person.flows.map(startedAt)) : -Infinity;
  const flows = running.flows.filter((flow) => !waiting.has(flow.flowId) && startedAt(flow) > oldest);
  return { flows, count: flows.length, more: running.more || person.more };
}

const startedAt = (flow: FlowSearchRow) => Date.parse(flow.startTime ?? "") || 0;

/** The groups of every Flow type the list reads, merged newest first; a group unknown for one is unknown. */
export function tallyHealth(perFlowType: readonly GroupPages[]): RunHealth {
  const tallies = perFlowType.map((pages) => {
    const person = tally(pages.person);
    return {
      person,
      progress: inProgress(tally(pages.progress), person),
      failed: tally(pages.failed),
      done: tally(pages.done),
    };
  });
  const groups = Object.fromEntries(
    RUN_GROUP_KEYS.map((key) => {
      const parts = tallies.map((each) => each[key]);
      if (parts.some((part) => part === null)) return [key, null];
      const known = parts as GroupTally[];
      return [
        key,
        {
          flows: known.flatMap((part) => part.flows).sort((a, b) => startedAt(b) - startedAt(a)),
          count: known.reduce((sum, part) => sum + part.count, 0),
          more: known.some((part) => part.more),
        },
      ];
    }),
  ) as Record<RunGroupKey, GroupTally | null>;
  return { groups, waiting: groups.person && new Set(groups.person.flows.map((flow) => flow.flowId)) };
}

/** A count as a person reads it: a floor says so, as "50+". */
export function tallyText(group: GroupTally): string {
  return group.more ? `${group.count}+` : String(group.count);
}
