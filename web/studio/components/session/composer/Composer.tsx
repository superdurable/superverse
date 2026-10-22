// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import {
  useEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
} from "react";
import { Button } from "@/components/ui/Button";
import { Glyph, type GlyphName } from "@/components/ui/Glyph";
import type { AttachmentTray as Tray } from "@/lib/session/attachments/use-attachment-tray";
import { AttachButton } from "./attachments/AttachButton";
import { AttachmentTray } from "./attachments/AttachmentTray";

/** A control the composer will have, in its place and named, that does nothing yet. */
function NotYet({ glyph, name }: { glyph: GlyphName; name: string }) {
  return (
    <button
      type="button"
      aria-disabled="true"
      aria-label={`${name}, coming soon`}
      title={`${name}: coming soon`}
      className="grid size-7 shrink-0 cursor-default place-items-center rounded-[7px] text-ink-faint"
    >
      <Glyph name={glyph} />
    </button>
  );
}

/**
 * Composer is the one input. Enter sends and Shift+Enter breaks the line: the
 * box holds a sentence far more often than a paragraph.
 *
 * onSend reports its own failures. A rejected send keeps the draft so the
 * person can retry it; a resolved send clears it. With onStop, a Stop button
 * beside it stops the running turn and tasks, and so does Escape. The compact
 * box's row reads as chat inputs do: attach and the element picker on the left;
 * the model, voice input and Send on the right. Voice input is a placeholder
 * for now.
 *
 * With `attachments`, the files the person attaches (Attach, or images pasted
 * into the text) sit as chips above the text, and the message carries their
 * IDs: Send waits until every chip is ready, and the text may then be blank.
 */
export function Composer({
  onSend,
  attachments,
  value,
  onChange,
  busy = false,
  disabledReason = null,
  sendDisabledReason = null,
  placeholder = "Describe a change...",
  autoFocus = false,
  inputRef,
  large = false,
  onStop,
  stopDisabled = false,
  stopping = false,
  about,
  onClearAbout,
  focusKey,
  picking,
  onPick,
  menu,
}: {
  /** Sends the message with its ready attachments, in chip order. */
  onSend: (text: string, attachmentIds: readonly string[]) => unknown;
  /** The composer's attachment tray; without it the composer attaches nothing. */
  attachments?: Tray;
  value?: string;
  onChange?: (text: string) => void;
  busy?: boolean;
  disabledReason?: string | null;
  /** Blocks sending while leaving the draft editable, for example before a local agent connects. */
  sendDisabledReason?: string | null;
  placeholder?: string;
  autoFocus?: boolean;
  /** The text box, for a caller that moves the focus there when it has a draft to show. */
  inputRef?: RefObject<HTMLTextAreaElement | null>;
  large?: boolean;
  /** Shown while a turn or a task runs; omitted otherwise. */
  onStop?: () => unknown;
  stopDisabled?: boolean;
  /** The Session admitted a Stop and its work is ending. */
  stopping?: boolean;
  /** What the message is about, shown as a chip above the text; the caller puts it in the message. */
  about?: string | null;
  onClearAbout?: () => void;
  /** A new value moves the focus into the text box. */
  focusKey?: number;
  /** The element picker: point at a Step, run or item to ask about it. */
  picking?: boolean;
  onPick?: () => void;
  /** A menu before the voice input and Send, such as the Session's Agent picker with its model. */
  menu?: ReactNode;
}) {
  const [localText, setLocalText] = useState("");
  const ownTextarea = useRef<HTMLTextAreaElement | null>(null);
  const textarea = inputRef ?? ownTextarea;
  const text = value ?? localText;
  const disabled = busy || disabledReason !== null;
  const chips = attachments?.chips.length ?? 0;
  // Files a message cannot carry, such as while a local agent runs the Session, hold it until removed.
  const attachmentsBlock = chips > 0 && attachments ? (attachments.disabledReason ?? attachments.waitReason) : null;
  const canSend =
    !disabled && sendDisabledReason === null && attachmentsBlock === null && (text.trim() !== "" || chips > 0);

  function changeText(next: string) {
    if (value === undefined) setLocalText(next);
    onChange?.(next);
  }

  // A disabled box cannot take the focus, so it moves there once the composer is enabled.
  useEffect(() => {
    if (autoFocus && !disabled) textarea.current?.focus();
  }, [autoFocus, disabled, textarea]);

  useEffect(() => {
    if (focusKey) textarea.current?.focus();
  }, [focusKey, textarea]);

  // Escape stops the running work, unless something else, such as a dialog, already took the key.
  useEffect(() => {
    if (!onStop || stopDisabled) return;
    const stopOnEscape = (event: globalThis.KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented || event.isComposing) return;
      event.preventDefault();
      void onStop();
    };
    window.addEventListener("keydown", stopOnEscape);
    return () => window.removeEventListener("keydown", stopOnEscape);
  }, [onStop, stopDisabled]);

  async function send() {
    if (!canSend) return;
    const message = text.trim();
    try {
      // The tray keeps the sent chips until the send settles, and forgets them once it is delivered.
      if (attachments) await attachments.send(async (attachmentIds) => onSend(message, attachmentIds));
      else await onSend(message, []);
      changeText("");
    } catch {
      // The draft stays for a retry; the caller already shows why it failed.
    }
  }

  function sendOnEnter(event: KeyboardEvent<HTMLTextAreaElement>) {
    // Enter that confirms an IME candidate (Chinese, Japanese, Korean input)
    // belongs to the composition, not to the message.
    if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing || event.keyCode === 229) return;
    event.preventDefault();
    void send();
  }

  // An image pasted into the text is attached; pasted text stays text.
  function attachPasted(event: ClipboardEvent<HTMLTextAreaElement>) {
    const images = [...event.clipboardData.files].filter((file) => file.type.startsWith("image/"));
    if (!attachments || images.length === 0) return;
    event.preventDefault();
    attachments.add(images);
  }

  const hint = disabledReason ?? (busy ? "A request is already running" : attachmentsBlock);
  const sendHint = disabledReason === null ? sendDisabledReason : null;

  if (large)
    return (
      <div className="w-full">
        {/* The shell carries the focus ring, so the borderless box inside it needs none of its own. */}
        <div className="rounded-xl border border-line bg-panel p-3 shadow-(--shadow-lift) focus-within:border-accent focus-within:shadow-(--focus-ring)">
          {attachments && (
            <AttachmentTray tray={attachments} className="mb-2" returnFocus={() => textarea.current?.focus()} />
          )}
          <div className="grid grid-cols-[minmax(0,1fr)_auto] items-end gap-2">
            <textarea
              ref={textarea}
              className="w-full resize-none bg-transparent p-1 text-[17px]/[1.625] text-ink outline-hidden placeholder:text-ink-faint disabled:cursor-not-allowed"
              aria-label="Describe a change"
              value={text}
              disabled={disabled}
              placeholder={placeholder}
              rows={3}
              onChange={(event) => changeText(event.target.value)}
              onKeyDown={sendOnEnter}
              onPaste={attachPasted}
            />
            <Button variant="primary" disabled={!canSend} onClick={() => void send()}>
              Build
            </Button>
          </div>
          <div className="flex items-center gap-1 pt-1">
            {attachments && <AttachButton tray={attachments} disabled={disabled} className="-ml-1" />}
            <p className="text-[13px] text-ink-faint">{sendHint ?? hint ?? "Enter to send"}</p>
          </div>
        </div>
      </div>
    );

  return (
    <div className="px-3 pt-2 pb-3">
      {/* The shell carries the focus ring, so the borderless box inside it needs none of its own. */}
      <div className="rounded-xl border border-line-soft bg-panel px-2.25 pt-2 pb-1.75 focus-within:border-accent-rule focus-within:shadow-(--focus-ring)">
        {about && (
          <span className="mb-1 inline-flex h-5.5 max-w-full items-center gap-1 rounded-md bg-accent-wash-strong pr-1 pl-2 text-xs text-accent">
            <span className="truncate">About: {about}</span>
            <button
              type="button"
              aria-label="Remove what the message is about"
              onClick={onClearAbout}
              className="grid size-4.5 place-items-center rounded-sm hover:bg-accent-wash"
            >
              <Glyph name="close" className="size-3" />
            </button>
          </span>
        )}
        {attachments && (
          <AttachmentTray tray={attachments} className="mb-1.5" returnFocus={() => textarea.current?.focus()} />
        )}
        <textarea
          ref={textarea}
          className="max-h-35 min-h-10 w-full resize-none bg-transparent text-[13.5px]/normal text-ink outline-hidden placeholder:text-ink-faint disabled:cursor-not-allowed"
          aria-label="Message the agent"
          value={text}
          disabled={disabled}
          placeholder={placeholder}
          rows={2}
          onChange={(event) => changeText(event.target.value)}
          onKeyDown={sendOnEnter}
          onPaste={attachPasted}
        />
        {sendHint && (
          <p role="status" className="mt-1 text-[12px] text-ink-dim">
            {sendHint}
          </p>
        )}
        <div className="mt-0.5 flex items-center gap-1">
          {attachments && <AttachButton tray={attachments} disabled={disabled} />}
          {onPick && (
            <button
              type="button"
              aria-label="Point at a step, run or item to ask about it"
              aria-pressed={picking}
              title="Point at a step, run or item to ask about it"
              onClick={onPick}
              className="grid size-7 place-items-center rounded-[7px] text-ink-faint hover:bg-hover hover:text-ink aria-pressed:bg-accent-wash-strong aria-pressed:text-accent"
            >
              <Glyph name="pick" />
            </button>
          )}
          <span className="ml-auto min-w-0 truncate px-1 text-[12px] text-ink-faint">
            {hint ?? (onStop ? "Esc to stop" : "")}
          </span>
          {onStop && (
            <Button
              size="sm"
              disabled={stopDisabled || stopping}
              onClick={() => void onStop()}
              aria-keyshortcuts="Escape"
            >
              <Glyph name="close" className="size-3" />
              {stopping ? "Stopping…" : "Stop"}
            </Button>
          )}
          {menu}
          <NotYet glyph="mic" name="Voice input" />
          <button
            type="button"
            aria-label="Send"
            title="Send (Enter). While the agent works, the message waits in the queue."
            disabled={!canSend}
            onClick={() => void send()}
            className="grid size-7 shrink-0 place-items-center rounded-lg bg-ink text-panel disabled:cursor-default disabled:bg-band-1 disabled:text-ink-faint"
          >
            <Glyph name="arrowUp" />
          </button>
        </div>
      </div>
    </div>
  );
}
