// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { Button } from "@/components/ui/Button";

export interface BriefActionsProps {
  disabled: boolean;
  submitting: boolean;
  /** Why the last answer or message was not taken. */
  error?: string | null;
  /** Confirm and build, only while the card waits for the person. */
  onConfirm?: () => void;
  /**
   * Sends what should change: the waiting card's request for changes, which the agent answers with a revised
   * card, or else a message to the agent. The box closes once it is taken.
   */
  onChange: (text: string) => Promise<unknown>;
}

/** The Brief's foot: Change something…, which opens a small box in place, and Confirm and build while the card waits. */
export function BriefActions({ disabled, submitting, error, onConfirm, onChange }: BriefActionsProps) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const changes = text.trim();
  const send = () => {
    if (disabled || changes === "") return;
    onChange(changes).then(
      () => {
        setText("");
        setOpen(false);
      },
      () => undefined,
    );
  };
  return (
    <div className="grid gap-2 border-t border-line-soft bg-panel px-5 py-3" aria-busy={submitting}>
      {open && (
        <textarea
          aria-label="What should change"
          className="min-h-16 w-full resize-y rounded-lg border border-line bg-panel px-3 py-2 text-body text-ink placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          placeholder="Say what should change…"
          autoFocus
          value={text}
          disabled={disabled}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) send();
            if (event.key === "Escape") setOpen(false);
          }}
        />
      )}
      {error && (
        <p role="alert" className="text-caption text-fail">
          {error}
        </p>
      )}
      <div className="flex flex-wrap items-center justify-end gap-2">
        {open ? (
          <>
            <Button variant="quiet" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button variant="accent" disabled={disabled || changes === ""} onClick={send}>
              Send changes
            </Button>
          </>
        ) : (
          <Button aria-expanded={false} onClick={() => setOpen(true)}>
            Change something…
          </Button>
        )}
        {onConfirm && !open && (
          <Button variant="primary" disabled={disabled} onClick={onConfirm}>
            Confirm and build
          </Button>
        )}
      </div>
    </div>
  );
}
