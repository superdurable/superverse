// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

/** What another surface asks the chat's composer to hold: the element it is about, and optional text. */
export interface ComposerProposal {
  /** "Weekly send ▸ Editor approves": shown as a chip and put in front of the message. */
  about: string;
  text?: string;
  /** Increases with every proposal, so the composer takes each one once. */
  id: number;
}

interface ComposerDraft {
  proposal: ComposerProposal | null;
  /** Puts `about` (and `text`) into the composer and calls the frame's `onPropose`, which opens the chat. */
  propose: (about: string, text?: string) => void;
  /** The person is pointing at a Step, run or item to ask about, with the composer's picker. */
  picking: boolean;
  setPicking: (picking: boolean) => void;
  /** Increases each time something asks for the composer's focus, such as the / key. */
  focusRequests: number;
  requestFocus: () => void;
}

const ComposerDraftContext = createContext<ComposerDraft | null>(null);

/**
 * Lets a surface hand the Project's chat something to ask about: a Step, a run or an Inbox item, and
 * for a failed Live run the error to fix on Preview. The chat's composer shows it as a chip and puts
 * it in front of the message; nothing is sent until the person sends.
 */
export function ComposerDraftProvider({ onPropose, children }: { onPropose?: () => void; children: ReactNode }) {
  const [proposal, setProposal] = useState<ComposerProposal | null>(null);
  const [picking, setPicking] = useState(false);
  const [focusRequests, setFocusRequests] = useState(0);
  const propose = useCallback(
    (about: string, text?: string) => {
      setProposal((current) => ({ about, text, id: (current?.id ?? 0) + 1 }));
      onPropose?.();
    },
    [onPropose],
  );
  const requestFocus = useCallback(() => {
    setFocusRequests((count) => count + 1);
    onPropose?.();
  }, [onPropose]);
  const value = useMemo(
    () => ({ proposal, propose, picking, setPicking, focusRequests, requestFocus }),
    [proposal, propose, picking, focusRequests, requestFocus],
  );
  return <ComposerDraftContext.Provider value={value}>{children}</ComposerDraftContext.Provider>;
}

/** The composer draft of the page, or null outside a Project's Builder. */
export function useComposerDraft() {
  return useContext(ComposerDraftContext);
}

/** While `enabled`, "/" opens the chat and puts the cursor in the composer, unless the person is typing elsewhere. */
export function useSlashFocusesComposer(enabled: boolean) {
  const requestFocus = useComposerDraft()?.requestFocus;
  useEffect(() => {
    if (!enabled || !requestFocus) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, [contenteditable='true']")) return;
      event.preventDefault();
      requestFocus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enabled, requestFocus]);
}

/**
 * What the chat makes of the proposals other surfaces send: the chip it shows (`about`) and a key
 * that moves the focus to the composer, with the proposal's text handed to `onText`. Each proposal is
 * taken once, while the chat renders.
 */
export function useComposerProposal(onText: (text: string) => void) {
  const draft = useComposerDraft();
  const [about, setAbout] = useState<string | null>(null);
  const [focusKey, setFocusKey] = useState(0);
  const proposal = draft?.proposal ?? null;
  const [taken, setTaken] = useState(0);
  if (proposal && proposal.id !== taken) {
    setTaken(proposal.id);
    setAbout(proposal.about);
    if (proposal.text) onText(proposal.text);
    setFocusKey((key) => key + 1);
  }
  const clearAbout = useCallback(() => setAbout(null), []);
  return { about, clearAbout, focusKey, draft };
}
