// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import type { AttachmentFileLinks } from "@/lib/session/agent-types";

/*
 * The conversation reaches its attachments' files only through the links its page provides: the shared Agent
 * UI stays transport-free, and the Session page injects links that call GetAttachmentDownloads.
 */

const AttachmentFilesContext = createContext<AttachmentFileLinks | null>(null);

export function AttachmentFilesProvider({ links, children }: { links: AttachmentFileLinks; children: ReactNode }) {
  return <AttachmentFilesContext.Provider value={links}>{children}</AttachmentFilesContext.Provider>;
}

/** Waits before asking again for a thumbnail the Session has not shown yet: its copy is made when it is. */
const THUMBNAIL_RETRIES_MS = [2_000, 5_000, 10_000, 20_000, 40_000, 60_000];

/**
 * The inline URL of an image attachment, for its thumbnail, or null while it has none. An image that fails to
 * load (`reload`), such as after its URL expired, asks for a new one once.
 */
export function useAttachmentThumbnail(attachmentId: string, wanted: boolean) {
  const links = useContext(AttachmentFilesContext);
  const [shown, setShown] = useState<{ attachmentId: string; url: string } | null>(null);
  const [round, setRound] = useState(0);
  useEffect(() => {
    if (!links || !wanted) return;
    let canceled = false;
    let timer: number | undefined;
    const load = (attempt: number) =>
      void links.link(attachmentId, "inline").then((link) => {
        if (canceled) return;
        if (link) setShown({ attachmentId, url: link.url });
        else if (attempt < THUMBNAIL_RETRIES_MS.length)
          timer = window.setTimeout(() => load(attempt + 1), THUMBNAIL_RETRIES_MS[attempt]);
      });
    load(0);
    return () => {
      canceled = true;
      window.clearTimeout(timer);
    };
  }, [attachmentId, links, round, wanted]);
  // One more try after a failed load: an image that fails again shows its file icon.
  const reload = useCallback(() => {
    setShown(null);
    setRound((current) => Math.min(current + 1, 1));
  }, []);
  return { url: shown?.attachmentId === attachmentId ? shown.url : null, reload };
}

/**
 * Opens an attachment as a download with its file name, or null when the page provides no links. `warm` asks
 * for the link ahead of a click, as when the pointer reaches the chip.
 */
export function useAttachmentDownload() {
  const links = useContext(AttachmentFilesContext);
  return links
    ? {
        warm: (attachmentId: string) => void links.link(attachmentId, "download"),
        open: async (attachmentId: string): Promise<boolean> => {
          const link = await links.link(attachmentId, "download");
          // The download answers with Content-Disposition: attachment, so the page stays where it is.
          if (link) window.location.assign(link.url);
          return link !== null;
        },
      }
    : null;
}
