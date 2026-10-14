// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/** `value` as a local date in the viewer's locale, or `unreadable` when it is not a date. */
function formatDateWith(value: string, options: Intl.DateTimeFormatOptions, unreadable: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return unreadable;
  return new Intl.DateTimeFormat(undefined, options).format(date);
}

export function formatDate(value: string | null): string {
  return value ? formatDateWith(value, { dateStyle: "medium", timeStyle: "medium" }, "—") : "—";
}

/** Time only, for a narrow column where the date would not fit and rarely differs. */
export function formatTimeOfDay(value: string | null): string {
  return value ? formatDateWith(value, { timeStyle: "short" }, "—") : "—";
}

/** Month, day and minute, for a message stamp; an unreadable value is shown as written. */
export function formatMonthDayTime(value: string): string {
  return formatDateWith(value, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }, value);
}

/** A positive Unix timestamp in seconds as a local date, or undefined when absent or invalid. */
export function formatUnixSeconds(value: unknown): string | undefined {
  if (value === undefined || value === null || value === "") return undefined;
  const timestamp = Number(value);
  if (!Number.isFinite(timestamp) || timestamp <= 0) return undefined;
  return formatDate(new Date(timestamp * 1000).toISOString());
}

export function formatDuration(start: string | null, close: string | null): string {
  if (!start) return "—";
  const startMs = Date.parse(start);
  const endMs = close ? Date.parse(close) : Date.now();
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs)) return "—";
  const seconds = Math.max(0, Math.floor((endMs - startMs) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

/** A measured duration, with sub-second and tenth-of-a-second precision while it is short. */
export function formatElapsedDuration(milliseconds: number | null): string {
  if (milliseconds === null || !Number.isFinite(milliseconds)) return "";
  const elapsed = Math.max(0, milliseconds);
  if (elapsed < 1000) return `${Math.round(elapsed)}ms`;
  if (elapsed < 10_000) return `${Number((elapsed / 1000).toFixed(1))}s`;
  const seconds = Math.round(elapsed / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

/**
 * Whole seconds with a zero-padded lower unit, so a ticking value keeps its width: <1s, 42s, 1m 05s, 2h 03m.
 * Null when absent, negative or not finite.
 */
export function formatPaddedDuration(milliseconds: number | null): string | null {
  if (milliseconds === null || !Number.isFinite(milliseconds) || milliseconds < 0) return null;
  const seconds = Math.floor(milliseconds / 1_000);
  if (seconds < 1) return "<1s";
  if (seconds < 60) return `${String(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${String(minutes)}m ${String(seconds % 60).padStart(2, "0")}s`;
  return `${String(Math.floor(minutes / 60))}h ${String(minutes % 60).padStart(2, "0")}m`;
}

/** Coarse elapsed: no seconds above a minute, no minutes above a day. */
export function formatCoarseElapsed(fromMs: number, nowMs: number): string {
  const seconds = Math.max(0, Math.round((nowMs - fromMs) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

/** How long ago `value` was, in words: "just now", "6 min ago", "3 h ago", then the month and day. */
export function formatTimeAgo(value: string | null, nowMs: number): string {
  if (!value) return "";
  const at = Date.parse(value);
  if (Number.isNaN(at)) return "";
  const minutes = Math.floor(Math.max(0, nowMs - at) / 60_000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} h ago`;
  return formatDateWith(value, { month: "short", day: "numeric" }, value);
}

/**
 * A Process's name from its Flow Type, for people: `TextSummaryFlow` reads "Text summary". The Flow
 * Type itself stays the identifier wherever it can be copied or searched.
 */
export function processName(flowType: string): string {
  return inWords(flowType.replace(/Flow$/, "")) || flowType;
}

/** A Step type as people say it: `RecordAIFailure` is "Record AI failure", `listCommits` "List commits". */
export function stepName(stepType: string): string {
  const words = inWords(stepType);
  return words === "" ? stepType : words.charAt(0).toUpperCase() + words.slice(1);
}

/** An identifier split into sentence-case words, acronyms kept; empty when it has no words. */
function inWords(identifier: string): string {
  const words = identifier
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .split(/[\s_-]+/)
    .filter(Boolean);
  return words.map((word, index) => (index === 0 || /^[A-Z0-9]{2,}$/.test(word) ? word : word.toLowerCase())).join(" ");
}

/** The whole minutes left until `deadline`, rounded up. */
export function formatRemainingTime(deadline: string, now: number): string {
  const remainingMs = Math.max(0, Date.parse(deadline) - now);
  const totalMinutes = Math.ceil(remainingMs / 60_000);
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
}

/** An ISO time as a `datetime-local` input value in the viewer's time zone; empty when unreadable. */
export function toDateTimeLocalInput(value: string): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}

/** A Flow display field: strings as written, anything else as indented JSON. */
export function formatDisplayValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "string") return value;
  return JSON.stringify(value, null, 2);
}
