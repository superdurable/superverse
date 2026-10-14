// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useId, useState } from "react";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

export interface PendingMessageQueueItem {
  kind: "queued" | "steered";
  id: string;
  content: string;
  label: string;
}

/** What a person can do with a queued message: steer it in now, take it back to edit, or delete it. */
interface QueuedMessageActions {
  onSteer: (messageId: string) => void;
  onEdit: (messageId: string) => void;
  onDelete: (messageId: string) => void;
}

export interface PendingMessageQueueProps extends QueuedMessageActions {
  items: readonly PendingMessageQueueItem[];
  disabled: boolean;
}

// Queued and steered messages are hueless: waiting on the model needs no person.
const ITEM_KIND = {
  queued: cx("border-line-soft bg-panel"),
  steered: cx("border-line bg-well"),
} satisfies Record<PendingMessageQueueItem["kind"], string>;

export function PendingMessageQueue({ items, disabled, ...actions }: PendingMessageQueueProps) {
  const [isExpanded, setIsExpanded] = useState(true);
  const itemsID = useId();
  if (items.length === 0) return null;

  const queuedCount = items.filter((item) => item.kind === "queued").length;
  const steeringCount = items.length - queuedCount;
  return (
    <section className="rounded-xl border border-line-soft bg-well text-ink-dim" aria-label="Message queue">
      <button
        type="button"
        className="grid w-full cursor-pointer grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 rounded-xl px-2.5 py-2 text-left text-xs font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
        aria-controls={itemsID}
        aria-expanded={isExpanded}
        onClick={() => {
          setIsExpanded((current) => !current);
        }}
      >
        <span className="text-[10px] tracking-wider text-ink-faint uppercase">Message queue</span>
        <strong className="min-w-0 text-right font-semibold">
          {String(queuedCount)} queued · {String(steeringCount)} steering
        </strong>
        <span aria-hidden="true">{isExpanded ? "▴" : "▾"}</span>
      </button>
      {isExpanded && (
        <div
          className="grid max-h-[min(112px,16dvh)] gap-1 overflow-y-auto overscroll-contain px-1.5 pb-1.5"
          id={itemsID}
        >
          {items.map((item) => (
            <QueueItem disabled={disabled} item={item} key={`${item.kind}:${item.id}`} {...actions} />
          ))}
        </div>
      )}
    </section>
  );
}

function QueueItem({
  item,
  disabled,
  onSteer,
  onEdit,
  onDelete,
}: QueuedMessageActions & {
  item: PendingMessageQueueItem;
  disabled: boolean;
}) {
  return (
    <div
      className={cx(
        "grid min-h-8 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 rounded-lg border px-2 py-0.5",
        ITEM_KIND[item.kind],
      )}
    >
      <div className="inline-flex items-center gap-1 text-[10px] whitespace-nowrap text-ink-faint">
        <strong className="font-semibold uppercase">{item.label}</strong>
      </div>
      <p className="min-w-0 truncate text-[11px] leading-tight">{item.content}</p>
      {item.kind === "queued" && (
        <div className="flex items-center gap-1 whitespace-nowrap">
          <Button variant="accent" size="sm" disabled={disabled} onClick={() => onSteer(item.id)}>
            <span aria-hidden="true">↪</span>
            Steer now
          </Button>
          <Button variant="quiet" size="sm" disabled={disabled} onClick={() => onEdit(item.id)}>
            Edit
          </Button>
          <Button variant="quiet" size="sm" disabled={disabled} onClick={() => onDelete(item.id)}>
            Delete
          </Button>
        </div>
      )}
    </div>
  );
}
