// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useId, useState } from "react";
import { RequestProblem } from "@/components/publishing/RequestProblem";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";

/** The longest reason the server keeps with a decline, in characters. */
const REASON_LIMIT = 500;

/**
 * Decline's confirmation, the same from the Goals tab and the chat: what declining does, an optional reason that later
 * reviews read, and Decline proposal or Not now. It stays open while the decline is sent and after a failure, so the
 * reason is kept for a retry; an answer closes it. The reason's hint describes it, turns into its error when it is too
 * long, which is announced once, and blocks Decline proposal until it is short enough.
 */
export function DeclineDialog({
  title,
  declining,
  failure,
  onDecline,
  onClose,
}: {
  title: string;
  declining: boolean;
  /** Why the last decline failed, shown above the buttons; null when it did not. */
  failure: unknown;
  /** Sends the decline with the trimmed reason; resolves to whether the server answered. */
  onDecline: (reason: string) => Promise<boolean>;
  onClose: () => void;
}) {
  const [reason, setReason] = useState("");
  const hint = useId();
  const trimmed = reason.trim();
  const length = [...trimmed].length;
  const tooLong = length > REASON_LIMIT;
  const decline = async () => {
    if (tooLong || declining) return;
    if (await onDecline(trimmed)) onClose();
  };
  return (
    <Dialog onDismiss={onClose} aria-label="Decline proposal" data-testid="decline-proposal-dialog">
      <form
        className="grid gap-3 px-5 py-4 text-body"
        onSubmit={(event) => {
          event.preventDefault();
          void decline();
        }}
      >
        <h2 className="text-title font-semibold text-ink">Decline &ldquo;{title}&rdquo;?</h2>
        <p className="text-ink-dim">
          The agent drops the change. Later reviews read your reason and propose it again only when the numbers change.
        </p>
        <div className="grid gap-1 text-caption">
          <label className="grid gap-1 text-ink-dim">
            Why not?
            <textarea
              className="min-h-20 rounded-md border border-line-soft bg-panel px-2 py-1.5 text-body text-ink focus-visible:outline-2 focus-visible:outline-accent"
              value={reason}
              aria-invalid={tooLong}
              aria-describedby={hint}
              aria-errormessage={tooLong ? hint : undefined}
              onChange={(event) => setReason(event.target.value)}
              placeholder="For example: we want the second follow-up for strong-fit prospects"
            />
          </label>
          <p id={hint} className={tooLong ? "text-fail" : "text-ink-faint"}>
            {tooLong
              ? `At most ${REASON_LIMIT} characters; this is ${length}.`
              : `Optional, at most ${REASON_LIMIT} characters.`}
          </p>
          {/* Announced when the reason grows too long, without the count each keystroke changes. */}
          <p aria-live="polite" className="sr-only">
            {tooLong && `The reason is too long: at most ${REASON_LIMIT} characters.`}
          </p>
        </div>
        {failure !== null && <RequestProblem operation="Declining the proposal" reason={failure} />}
        <div className="flex justify-end gap-2 border-t border-line-faint pt-3">
          <Button onClick={onClose}>Not now</Button>
          <Button type="submit" variant="danger" disabled={tooLong || declining}>
            {declining ? "Declining…" : "Decline proposal"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
