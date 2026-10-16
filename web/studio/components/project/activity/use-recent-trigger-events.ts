// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { listRecentTriggerEvents } from "@/lib/api/project";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { RecentTriggerEvent__Output } from "@/lib/generated/protobuf/superverse/v2/RecentTriggerEvent";
import { timestampToISO } from "@/lib/shared/protobuf";

/** The relay keeps no Watch: the list is read again at this interval while the page is visible. */
const POLL_MS = 10_000;

export interface RecentTriggerEvents {
  events: RecentTriggerEvent__Output[];
  /** When the server began recording; an empty list proves only that nothing arrived since. */
  bufferStartedAt: string | null;
  /** Random per server process; a change between reads means another instance answered. */
  bufferInstanceId: string;
  /** The list now comes from another server instance than the one before. */
  instanceSwitched: boolean;
  error: string;
  loaded: boolean;
}

/** The scope's recent trigger events, read every 10 seconds while `open` and the page is visible. */
export function useRecentTriggerEvents(
  projectId: string,
  scope: "PROJECT_SCOPE_PREVIEW" | "PROJECT_SCOPE_LIVE",
  open: boolean,
): RecentTriggerEvents {
  const [read, setRead] = useState<RecentTriggerEvents & { key: string }>({
    key: "",
    events: [],
    bufferStartedAt: null,
    bufferInstanceId: "",
    instanceSwitched: false,
    error: "",
    loaded: false,
  });
  const key = `${projectId}/${scope}`;
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const load = () => {
      if (document.visibilityState !== "visible") {
        timer = setTimeout(load, POLL_MS);
        return;
      }
      listRecentTriggerEvents({ projectId, scope }, controller.signal).then(
        (response) => {
          if (controller.signal.aborted) return;
          setRead((current) => ({
            key,
            events: response.events,
            bufferStartedAt: timestampToISO(response.bufferStartedAt),
            bufferInstanceId: response.bufferInstanceId,
            instanceSwitched:
              current.key === key && current.loaded && current.bufferInstanceId !== response.bufferInstanceId,
            error: "",
            loaded: true,
          }));
          timer = setTimeout(load, POLL_MS);
        },
        (reason: unknown) => {
          if (controller.signal.aborted) return;
          setRead((current) => ({
            ...current,
            key,
            error: errorMessage(reason, "Could not read the recent events"),
            loaded: true,
          }));
          timer = setTimeout(load, POLL_MS);
        },
      );
    };
    load();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [key, open, projectId, scope]);
  return read.key === key
    ? read
    : {
        events: [],
        bufferStartedAt: null,
        bufferInstanceId: "",
        instanceSwitched: false,
        error: "",
        loaded: false,
      };
}
