// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { newRequestId } from "@/lib/api/transport/api-error";
import {
  readStored,
  readStoredPreference,
  removeStored,
  STORAGE_KEYS,
  writeStored,
  writeStoredPreference,
} from "@/lib/browser/storage";
import { ATTACHMENT_LIMITS, attachmentFormat, LOCAL_REFUSALS } from "./attachment-files";
import {
  chipsReducer,
  chipWorking,
  newChip,
  readRemembered,
  rememberedChip,
  rememberedOf,
  type AttachmentChip,
  type ChipPatch,
  type RememberedAttachment,
} from "./attachment-chips";
import { discardAttachment, settleAttachment, uploadAttachmentFile } from "./attachment-upload";

/** The attachments of one composer: its chips and what the person and the composer do with them. */
export interface AttachmentTray {
  chips: readonly AttachmentChip[];
  /** Adds files; an eleventh, and every file while attaching is unavailable, is refused with a note. */
  add: (files: readonly File[]) => void;
  /** Removes a chip: its work stops and its staged attachment is deleted. A chip a send carries stays. */
  remove: (key: string) => void;
  /**
   * Sends a message with the ready attachments: `deliver` gets their IDs in chip order, and while it runs
   * those chips cannot be removed, while files added meanwhile wait for the next message. Once it resolves
   * the sent chips are forgotten, since the message owns their attachments; a rejection keeps them and asks
   * the server again where each stands, so that one that expired or went shows it.
   */
  send: (deliver: (attachmentIds: readonly string[]) => Promise<unknown>) => Promise<void>;
  /** The keys of the chips a send in flight carries. */
  sending: ReadonlySet<string>;
  /** Shows attachments again, such as those of a message taken back from the queue, and checks each. */
  restore: (entries: readonly RememberedAttachment[]) => void;
  /** Whether every chip is ready, so the message may be sent. */
  ready: boolean;
  /** The ready attachments, in chip order. */
  attachmentIds: readonly string[];
  /** The ready chips as the browser remembers them, for a creation that carries them. */
  remembered: readonly RememberedAttachment[];
  /** Why a message waits for its files, or null. */
  waitReason: string | null;
  /** The last refusal of an attach (too many files, attaching unavailable), or null. */
  notice: string | null;
  /** Whether to show, once per browser, that the AI model reads attached files. */
  disclosure: boolean;
  /** Why attaching is unavailable, or null. */
  disabledReason: string | null;
}

const NOTICE_MS = 6000;
const NOTHING_SENDING: ReadonlySet<string> = new Set();

function readEntries(scope: string): RememberedAttachment[] {
  try {
    const encoded = readStored("session", STORAGE_KEYS.composerAttachments(scope));
    return encoded ? readRemembered(JSON.parse(encoded)) : [];
  } catch {
    return [];
  }
}

function writeEntries(scope: string, entries: readonly RememberedAttachment[]) {
  try {
    if (entries.length) writeStored("session", STORAGE_KEYS.composerAttachments(scope), JSON.stringify(entries));
    else removeStored("session", STORAGE_KEYS.composerAttachments(scope));
  } catch {
    // Blocked storage: a reload forgets the chips, whose attachments expire unsent.
  }
}

/**
 * The attachment tray of a composer in `scope` (a Project, or the start page of a Teamspace), uploading into
 * `teamSpaceId`, unknown while the page reads it. Chips whose attachment exists are remembered in session
 * storage, so a reload shows them again and asks CompleteAttachmentUpload where each stands. Unmounting
 * aborts every PUT and wait; nothing is deleted then, since the remembered chips come back.
 */
export function useAttachmentTray({
  teamSpaceId,
  scope,
  disabledReason = null,
}: {
  teamSpaceId: string | null;
  scope: string;
  disabledReason?: string | null;
}): AttachmentTray {
  const [chips, dispatch] = useReducer(chipsReducer, []);
  const [restoredScope, setRestoredScope] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [disclosure, setDisclosure] = useState(false);
  // The chips a send in flight carries: the ref answers remove at once, the state renders them locked.
  const [sending, setSending] = useState<ReadonlySet<string>>(NOTHING_SENDING);
  const inFlight = useRef<ReadonlySet<string>>(NOTHING_SENDING);
  // The running work of each chip, the object URL of each image and the chips removed while registering.
  const work = useRef(new Map<string, AbortController>());
  const previews = useRef(new Map<string, string>());
  const discardOnRegister = useRef(new Set<string>());
  const noticeTimer = useRef<number | undefined>(undefined);

  const reporter = useCallback(
    (space: string, key: string) => (patch: ChipPatch) => {
      if (patch.attachmentId && discardOnRegister.current.delete(key)) {
        work.current.get(key)?.abort();
        work.current.delete(key);
        discardAttachment(space, patch.attachmentId);
        return;
      }
      if (work.current.has(key)) dispatch({ type: "patched", key, patch });
    },
    [],
  );

  // Asks the server where the attachment of each chip stands, replacing the chip's earlier work.
  const settle = useCallback(
    (space: string, entries: readonly { key: string; attachmentId: string }[]) => {
      for (const entry of entries) {
        work.current.get(entry.key)?.abort();
        const controller = new AbortController();
        work.current.set(entry.key, controller);
        void settleAttachment(space, entry.attachmentId, false, reporter(space, entry.key), controller.signal);
      }
    },
    [reporter],
  );

  // Shows remembered chips at once and asks the server where each stands.
  const check = useCallback(
    (space: string, entries: readonly RememberedAttachment[]) => {
      dispatch({ type: "added", chips: entries.map(rememberedChip) });
      settle(
        space,
        entries.map((entry) => ({ key: entry.uploadRequestId, attachmentId: entry.attachmentId })),
      );
    },
    [settle],
  );

  // A new scope or Teamspace starts from what the browser remembers of it.
  useEffect(() => {
    if (!teamSpaceId) return;
    const running = work.current;
    const urls = previews.current;
    const restore = window.setTimeout(() => {
      dispatch({ type: "cleared" });
      check(teamSpaceId, readEntries(scope));
      setRestoredScope(scope);
    }, 0);
    return () => {
      window.clearTimeout(restore);
      for (const controller of running.values()) controller.abort();
      running.clear();
      for (const url of urls.values()) URL.revokeObjectURL(url);
      urls.clear();
      setRestoredScope(null);
    };
  }, [check, scope, teamSpaceId]);

  // Remembered once restored, so that the first, empty render never erases what a reload brings back.
  useEffect(() => {
    if (restoredScope === scope)
      writeEntries(
        scope,
        chips.flatMap((chip) => rememberedOf(chip) ?? []),
      );
  }, [chips, restoredScope, scope]);

  useEffect(() => () => window.clearTimeout(noticeTimer.current), []);

  // A ready chip expires with its attachment's staging, while the page stays open.
  useEffect(() => {
    const ends = chips.flatMap((chip) => (chip.phase === "ready" && chip.expiresAt !== null ? [chip.expiresAt] : []));
    if (ends.length === 0) return;
    const expire = () => {
      for (const chip of chips)
        if (chip.phase === "ready" && chip.expiresAt !== null && chip.expiresAt <= Date.now())
          dispatch({ type: "patched", key: chip.key, patch: { phase: "expired" } });
    };
    // A timer waits at most about 24.8 days; a chip expires within a day.
    const timer = window.setTimeout(expire, Math.min(Math.max(0, Math.min(...ends) - Date.now()), 2 ** 31 - 1));
    return () => window.clearTimeout(timer);
  }, [chips]);

  const showNotice = useCallback((text: string) => {
    setNotice(text);
    window.clearTimeout(noticeTimer.current);
    noticeTimer.current = window.setTimeout(() => setNotice(null), NOTICE_MS);
  }, []);

  const add = useCallback(
    (files: readonly File[]) => {
      if (files.length === 0) return;
      if (disabledReason) return showNotice(disabledReason);
      if (!teamSpaceId || restoredScope !== scope) return showNotice("Reading the Project… try again in a moment");
      const room = ATTACHMENT_LIMITS.perMessage - chips.length;
      if (files.length > room) showNotice(LOCAL_REFUSALS.tooMany);
      const added = files.slice(0, Math.max(0, room)).map((file) => {
        const key = newRequestId();
        const format = attachmentFormat(file.name);
        const chip = newChip(key, file, format?.kind ?? "document", format?.mediaType ?? (file.type || "unknown"));
        if (!format) return { ...chip, phase: "failed" as const, failure: LOCAL_REFUSALS.unsupported };
        const controller = new AbortController();
        work.current.set(key, controller);
        let previewUrl: string | null = null;
        if (format.kind === "image") previews.current.set(key, (previewUrl = URL.createObjectURL(file)));
        const report = reporter(teamSpaceId, key);
        void uploadAttachmentFile({ teamSpaceId, requestId: key, file, format }, report, controller.signal);
        return { ...chip, previewUrl };
      });
      if (added.length === 0) return;
      dispatch({ type: "added", chips: added });
      // Storage that cannot be read shows the line again on the next page.
      if (readStoredPreference(STORAGE_KEYS.attachmentDisclosure) !== "shown") {
        setDisclosure(true);
        writeStoredPreference(STORAGE_KEYS.attachmentDisclosure, "shown");
      }
    },
    [chips.length, disabledReason, reporter, restoredScope, scope, showNotice, teamSpaceId],
  );

  const forget = useCallback((key: string) => {
    work.current.get(key)?.abort();
    work.current.delete(key);
    const url = previews.current.get(key);
    if (url) URL.revokeObjectURL(url);
    previews.current.delete(key);
  }, []);

  const remove = useCallback(
    (key: string) => {
      const chip = chips.find((item) => item.key === key);
      if (!chip || inFlight.current.has(key)) return;
      dispatch({ type: "removed", key });
      // A file still being declared is deleted once its ID is known.
      if (!chip.attachmentId && chip.phase === "registering" && work.current.has(key)) {
        discardOnRegister.current.add(key);
        return;
      }
      forget(key);
      if (chip.attachmentId && teamSpaceId) discardAttachment(teamSpaceId, chip.attachmentId);
    },
    [chips, forget, teamSpaceId],
  );

  const send = useCallback(
    async (deliver: (attachmentIds: readonly string[]) => Promise<unknown>) => {
      const sent = chips.filter((chip) => chip.phase === "ready" && chip.attachmentId);
      const keys: ReadonlySet<string> = new Set(sent.map((chip) => chip.key));
      inFlight.current = keys;
      setSending(keys);
      try {
        await deliver(sent.map((chip) => chip.attachmentId ?? ""));
        for (const key of keys) forget(key);
        dispatch({ type: "forgotten", keys });
        setDisclosure(false);
      } catch (reason) {
        if (teamSpaceId)
          settle(
            teamSpaceId,
            sent.map((chip) => ({ key: chip.key, attachmentId: chip.attachmentId ?? "" })),
          );
        throw reason;
      } finally {
        inFlight.current = NOTHING_SENDING;
        setSending(NOTHING_SENDING);
      }
    },
    [chips, forget, settle, teamSpaceId],
  );

  const restore = useCallback(
    (entries: readonly RememberedAttachment[]) => {
      if (!teamSpaceId) return;
      const shown = new Set(chips.map((chip) => chip.attachmentId));
      const room = ATTACHMENT_LIMITS.perMessage - chips.length;
      // A fresh chip key keeps a late answer for an earlier chip of the same attachment from reaching it.
      const unshown = entries.filter((entry) => !shown.has(entry.attachmentId));
      const fresh = unshown.slice(0, Math.max(0, room)).map((entry) => ({ ...entry, uploadRequestId: newRequestId() }));
      if (fresh.length < unshown.length) showNotice(LOCAL_REFUSALS.tooMany);
      check(teamSpaceId, fresh);
    },
    [check, chips, showNotice, teamSpaceId],
  );

  return useMemo(() => {
    const ready = chips.every((chip) => chip.phase === "ready");
    const working = chips.some(chipWorking);
    return {
      chips,
      add,
      remove,
      send,
      sending,
      restore,
      ready,
      attachmentIds: ready ? chips.flatMap((chip) => (chip.attachmentId ? [chip.attachmentId] : [])) : [],
      remembered: chips.flatMap((chip) => (chip.phase === "ready" ? (rememberedOf(chip) ?? []) : [])),
      waitReason: ready
        ? null
        : working
          ? "Waiting for the attached files…"
          : "Remove the files that could not be attached",
      notice,
      disclosure: disclosure && chips.length > 0,
      disabledReason,
    };
  }, [add, chips, disabledReason, disclosure, notice, remove, restore, send, sending]);
}
