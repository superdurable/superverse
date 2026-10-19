// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

const BASE_MS = 10_000;
const CAP_MS = 120_000;
const MAX_TIMER_MS = 2_147_483_647;

export type FlowSearchOutcome = "success" | "failure" | "unchanged";

export const FLOW_SEARCH_OUTCOME_SUCCESS: FlowSearchOutcome = "success";
export const FLOW_SEARCH_OUTCOME_FAILURE: FlowSearchOutcome = "failure";
export const FLOW_SEARCH_OUTCOME_UNCHANGED: FlowSearchOutcome = "unchanged";

/** Retry-After accepts delta seconds and HTTP dates; an invalid hint leaves exponential backoff in charge. */
function retryAfterMs(value: string | undefined): number {
  if (!value) return 0;
  const text = value.trim();
  if (/^\d+$/.test(text)) return Math.min(Number(text) * 1000, MAX_TIMER_MS);
  const date = Date.parse(text);
  return Number.isFinite(date) ? Math.min(Math.max(0, date - Date.now()), MAX_TIMER_MS) : 0;
}

/** One visible Runs or Inbox list owns one timer, shared by all its Flow type and group searches. */
export class FlowSearchBackoff {
  private timer: ReturnType<typeof setTimeout> | null = null;
  private active = false;
  private delayMs = BASE_MS;
  private readonly onDue: () => void;

  constructor(onDue: () => void) {
    this.onDue = onDue;
  }

  start(): void {
    this.active = true;
    this.schedule();
  }

  stop(): void {
    this.active = false;
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = null;
  }

  /** Manual reads run immediately; their result also moves the next quiet timer. */
  settled(outcome: FlowSearchOutcome, retryAfter?: string): void {
    if (!this.active) return;
    if (outcome === FLOW_SEARCH_OUTCOME_SUCCESS) this.delayMs = BASE_MS;
    if (outcome === FLOW_SEARCH_OUTCOME_FAILURE) {
      this.delayMs = Math.max(Math.min(this.delayMs * 2, CAP_MS), retryAfterMs(retryAfter));
    }
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = null;
    this.schedule();
  }

  private schedule(): void {
    if (!this.active || this.timer !== null) return;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.onDue();
    }, this.delayMs);
  }
}
