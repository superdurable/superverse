// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import { createFlowApiClient, type FlowApiClient } from "@/lib/api/transport/flow-api-client";
import type { ConfigurationItemKey__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItemKey";

/*
 * The autosave queue of one configuration scope in this tab. Every write is a compare-and-swap on the
 * scope's revision, so the queue writes one item at a time, each on the revision the previous write
 * returned. A text change waits 500 ms after the last keystroke; a selection writes at once. Apply
 * flushes the queue first and sends the revision it ends at.
 */

/** How long a typed change waits for the next keystroke before it is written. */
const TYPING_DELAY_MS = 500;

/** The kinds of configuration item, the server's item key kinds. */
type DraftItemKind = Exclude<ConfigurationItemKey__Output["kind"], "KIND_UNSPECIFIED">;

/** An item of a scope's configuration: its kind and the names that identify it. */
export interface DraftItemKey {
  kind: DraftItemKind;
  connectorId?: string;
  connectionName?: string;
  triggerName?: string;
  bindingName?: string;
  operationId?: string;
  flowType?: string;
  stepType?: string;
  environmentName?: string;
}

/** One string per item, the same for every editor of the item and for a gap that names it. */
export function draftItemKey(key: DraftItemKey): string {
  return [
    key.kind,
    key.connectorId,
    key.connectionName,
    key.triggerName,
    key.bindingName,
    key.operationId,
    key.flowType,
    key.stepType,
    key.environmentName,
  ]
    .map((part) => part ?? "")
    .join("/");
}

/** The element ID of the editor of an item, which a gap's link scrolls to and focuses. */
export function draftItemAnchor(key: string): string {
  return `setup-item-${key.replace(/[^A-Za-z0-9_-]/g, (char) => `-${char.charCodeAt(0).toString(16)}-`)}`;
}

/** The element ID of an item's Next publish control, which a link asking Live to set the item up focuses. */
export function nextPublishItemAnchor(key: string): string {
  return `${draftItemAnchor(key)}-next`;
}

/**
 * A write of one item, as its editor read it and as the server stores it. Its requests go through the
 * queue's own client, so a write still waiting when its editor closes is sent all the same.
 */
export interface DraftItem<T> {
  key: string;
  /** The item's value as this tab read it, at `revision`. */
  base: T;
  revision: string;
  /** Writes `value` on `revision`; answers the revision the write made. */
  write(client: FlowApiClient, value: T, revision: string): Promise<string>;
  /** The item as the server holds it now, after a write was refused for a changed revision. */
  read(client: FlowApiClient): Promise<{ value: T; revision: string }>;
  /** Whether two values are the same as the server tells them apart, by default equal JSON. */
  same?: (a: T, b: T) => boolean;
}

/** A write refused because the item changed elsewhere since this tab read it: yours and theirs. */
interface DraftConflict {
  key: string;
  yours: unknown;
  theirs: unknown;
}

type DraftStatus = "idle" | "saving" | "saved" | "failed";

/** What the bar and the editors show of the queue. */
export interface DraftSnapshot {
  status: DraftStatus;
  /** Why the last write failed, while the status is failed. */
  error: string;
  /** The items waiting or being written. */
  pending: readonly string[];
  conflicts: readonly DraftConflict[];
  /** The revision the newest write returned, null before this tab wrote. */
  revision: string | null;
}

/** A Flow API write refused because the scope's configuration revision changed. */
function isConfigurationRevisionConflict(reason: unknown): boolean {
  return reason instanceof ApiError && reason.code === "PROJECT_CONFIGURATION_REVISION_CONFLICT";
}

interface Entry {
  item: DraftItem<unknown>;
  /** The newest value this tab wrote or read, and the revision it holds at. */
  known: unknown;
  knownRevision: string;
  pending?: unknown;
  due: number;
  conflict?: { theirs: unknown; revision: string };
}

const newer = (a: string | null, b: string) => (a === null || BigInt(b) > BigInt(a) ? b : a);
const same = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

export class ConfigurationDraftQueue {
  /** The scope's own client, which no editor closes. */
  constructor(private readonly client: FlowApiClient) {}

  private readonly entries = new Map<string, Entry>();
  private snapshot: DraftSnapshot = { status: "idle", error: "", pending: [], conflicts: [], revision: null };
  private readonly listeners = new Set<() => void>();
  private timer: ReturnType<typeof setTimeout> | null = null;
  private running: Promise<void> | null = null;

  /** The queue's state; a new object on every change. */
  readonly getSnapshot = () => this.snapshot;

  readonly subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => void this.listeners.delete(listener);
  };

  /**
   * Changes an item: `update` gets the item's newest value in this tab, pending or written, and
   * answers the next one. A selection (`immediate`) writes at once; typing waits for a pause.
   */
  save<T>(item: DraftItem<T>, update: (current: T) => T, immediate: boolean) {
    const entry = this.entry(item as DraftItem<unknown>);
    const current = (entry.pending === undefined ? entry.known : entry.pending) as T;
    entry.pending = update(current);
    entry.due = immediate ? 0 : Date.now() + TYPING_DELAY_MS;
    this.publish();
    this.schedule();
  }

  /** The item's newest value in this tab, pending or written; undefined before this tab touched it. */
  valueOf(key: string): unknown {
    const entry = this.entries.get(key);
    return entry && (entry.pending === undefined ? entry.known : entry.pending);
  }

  /** Writes everything pending now and answers the revision the last write returned. */
  async flush(): Promise<string | null> {
    for (const entry of this.entries.values()) if (entry.pending !== undefined) entry.due = 0;
    this.schedule();
    while (this.running) await this.running;
    if (this.snapshot.status === "failed") throw new Error(this.snapshot.error);
    if (this.snapshot.conflicts.length > 0) throw new Error("Resolve the settings that changed elsewhere first.");
    return this.snapshot.revision;
  }

  /**
   * Learns a revision another write of the scope made, such as a Confirm all, so the next write is
   * sent on it instead of bouncing off a stale one.
   */
  observe(revision: string) {
    this.snapshot = { ...this.snapshot, revision: newer(this.snapshot.revision, revision) };
    this.publish();
  }

  /** Writes the failed items again. */
  retry() {
    for (const entry of this.entries.values()) if (entry.pending !== undefined) entry.due = 0;
    this.schedule();
  }

  /** Keeps this tab's value of a conflicting item (written on their revision) or takes theirs. */
  resolve(key: string, choice: "yours" | "theirs") {
    const entry = this.entries.get(key);
    if (!entry?.conflict) return;
    entry.known = entry.conflict.theirs;
    entry.knownRevision = entry.conflict.revision;
    if (choice === "theirs") delete entry.pending;
    else entry.due = 0;
    delete entry.conflict;
    this.publish();
    this.schedule();
  }

  private entry(item: DraftItem<unknown>): Entry {
    const existing = this.entries.get(item.key);
    if (!existing) {
      const entry: Entry = { item, known: item.base, knownRevision: item.revision, due: 0 };
      this.entries.set(item.key, entry);
      return entry;
    }
    existing.item = item;
    // A newer read of the item, with nothing of this tab's waiting, replaces what this tab knew.
    if (existing.pending === undefined && BigInt(item.revision) > BigInt(existing.knownRevision)) {
      existing.known = item.base;
      existing.knownRevision = item.revision;
    }
    return existing;
  }

  private schedule() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    if (this.running) return;
    const waiting = this.waiting();
    if (waiting.length === 0) return;
    const next = Math.max(0, Math.min(...waiting.map((entry) => entry.due)) - Date.now());
    if (next > 0) {
      this.timer = setTimeout(() => this.schedule(), next);
      return;
    }
    this.running = this.drain().finally(() => {
      this.running = null;
      this.schedule();
    });
  }

  private async drain() {
    for (;;) {
      const now = Date.now();
      const entry = this.waiting().find((candidate) => candidate.due <= now);
      if (!entry) return;
      this.snapshot = { ...this.snapshot, status: "saving", error: "" };
      this.publish();
      try {
        await this.write(entry);
      } catch (reason) {
        this.snapshot = { ...this.snapshot, status: "failed", error: errorMessage(reason, "The change was not saved") };
        entry.due = Number.POSITIVE_INFINITY;
        this.publish();
        return;
      }
      this.snapshot = { ...this.snapshot, status: "saved" };
      this.publish();
    }
  }

  /** The entries with a value to write: not in conflict, and not failed until a retry. */
  private waiting(): Entry[] {
    return [...this.entries.values()].filter(
      (entry) => entry.pending !== undefined && !entry.conflict && Number.isFinite(entry.due),
    );
  }

  /** Writes an entry's pending value; on a changed revision, again when the item itself is unchanged. */
  private async write(entry: Entry) {
    const value = entry.pending;
    const revision = newer(this.snapshot.revision, entry.knownRevision);
    try {
      this.wrote(entry, value, await entry.item.write(this.client, value, revision));
    } catch (reason) {
      if (!isConfigurationRevisionConflict(reason)) throw reason;
      const current = await entry.item.read(this.client);
      if (!(entry.item.same ?? same)(current.value, entry.known)) {
        entry.conflict = { theirs: current.value, revision: current.revision };
        this.snapshot = { ...this.snapshot, revision: newer(this.snapshot.revision, current.revision) };
        return;
      }
      this.wrote(entry, value, await entry.item.write(this.client, value, current.revision));
    }
  }

  private wrote(entry: Entry, value: unknown, revision: string) {
    entry.known = value;
    entry.knownRevision = revision;
    // A change made while the write ran stays pending.
    if (same(entry.pending, value)) delete entry.pending;
    this.snapshot = { ...this.snapshot, revision: newer(this.snapshot.revision, revision) };
  }

  private publish() {
    const entries = [...this.entries.values()];
    this.snapshot = {
      ...this.snapshot,
      pending: entries.filter((entry) => entry.pending !== undefined && !entry.conflict).map((entry) => entry.item.key),
      conflicts: entries.flatMap((entry) =>
        entry.conflict ? [{ key: entry.item.key, yours: entry.pending, theirs: entry.conflict.theirs }] : [],
      ),
    };
    for (const listener of this.listeners) listener();
  }
}

const queues = new Map<string, ConfigurationDraftQueue>();
const registryListeners = new Set<() => void>();
const IDLE: DraftSnapshot = { status: "idle", error: "", pending: [], conflicts: [], revision: null };
let combined = IDLE;

/** Every scope's queue in this tab as one state: the worst status, every pending item and conflict. */
function combine(): DraftSnapshot {
  const all = [...queues.values()].map((queue) => queue.getSnapshot());
  const status =
    (["failed", "saving", "saved"] as const).find((candidate) => all.some((item) => item.status === candidate)) ??
    "idle";
  return {
    status,
    error: all.find((item) => item.status === "failed")?.error ?? "",
    pending: all.flatMap((item) => item.pending),
    conflicts: all.flatMap((item) => item.conflicts),
    revision: null,
  };
}

/**
 * The queue of the configuration scope served at `basePath`, one per scope in this tab. Queues exist in
 * the browser only: a server render gets a queue of its own that nothing keeps.
 */
export function configurationDraftQueue(basePath: string): ConfigurationDraftQueue {
  if (typeof window === "undefined") return new ConfigurationDraftQueue(createFlowApiClient(basePath, {}));
  let queue = queues.get(basePath);
  if (!queue) {
    queue = new ConfigurationDraftQueue(createFlowApiClient(basePath, {}));
    queue.subscribe(() => {
      combined = combine();
      for (const listener of registryListeners) listener();
    });
    queues.set(basePath, queue);
  }
  return queue;
}

/** All of this tab's Setup writes as one state, for the bar and the leave prompt. */
export const configurationDrafts = {
  subscribe(listener: () => void) {
    registryListeners.add(listener);
    return () => void registryListeners.delete(listener);
  },
  getSnapshot: () => combined,
  getServerSnapshot: () => IDLE,
  /** Writes every scope's failed items again. */
  retry() {
    for (const queue of queues.values()) queue.retry();
  },
};
