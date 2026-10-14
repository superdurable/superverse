// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "@/lib/api/transport/api-error";

interface Page<T> {
  items: T[];
  nextPageToken: string;
}

interface Options<T> {
  /** Identity of the list (Project, filter, query): a new key starts an empty list. */
  key: string;
  fetchPage: (cursor: string, signal: AbortSignal) => Promise<Page<T>>;
  idOf: (item: T) => string;
  /** Items to show; others are dropped from every page. */
  keep?: (item: T) => boolean;
  /** Called with each page after it is accepted. */
  onPage?: (items: T[]) => void;
  enabled?: boolean;
  /** Delay before the first page, to debounce a search query. */
  delayMs?: number;
  /** Reloads the first page when it changes, keeping the shown items until then. */
  reloadKey?: unknown;
}

const NO_ITEMS: never[] = [];

/**
 * A cursor-paged list. Only the newest request may change the list: a refresh
 * aborts the request before it, a late response is dropped, appended pages
 * replace items with the same ID, and a cursor that repeats is an error.
 * Listing is discovery only; it never proves that an item is absent.
 */
export function usePagedList<T>({
  key,
  fetchPage,
  idOf,
  keep,
  onPage,
  enabled = true,
  delayMs = 0,
  reloadKey,
}: Options<T>) {
  const [list, setList] = useState<{ key: string; items: T[]; nextPageToken: string; loaded: boolean }>({
    key,
    items: [],
    nextPageToken: "",
    loaded: false,
  });
  const [status, setStatus] = useState({ key, loading: false, error: "" });
  const request = useRef<AbortController | null>(null);
  const latest = useRef({ fetchPage, idOf, keep, onPage });
  useEffect(() => {
    latest.current = { fetchPage, idOf, keep, onPage };
  });

  const load = useCallback(
    async (cursor: string, append: boolean) => {
      request.current?.abort();
      const controller = new AbortController();
      request.current = controller;
      setStatus({ key, loading: true, error: "" });
      try {
        const { fetchPage, idOf, keep, onPage } = latest.current;
        const page = await fetchPage(cursor, controller.signal);
        if (request.current !== controller) return;
        if (cursor && cursor === page.nextPageToken) throw new Error("The list returned a repeated cursor");
        const items = keep ? page.items.filter(keep) : page.items;
        setList((current) => {
          const previous = append && current.key === key ? current.items : [];
          return {
            key,
            items: [...new Map([...previous, ...items].map((item) => [idOf(item), item])).values()],
            nextPageToken: page.nextPageToken,
            loaded: true,
          };
        });
        setStatus({ key, loading: false, error: "" });
        onPage?.(items);
      } catch (reason) {
        if (request.current === controller && !controller.signal.aborted)
          setStatus({ key, loading: false, error: errorMessage(reason, "Could not load the list") });
      } finally {
        if (request.current === controller) request.current = null;
      }
    },
    [key],
  );

  useEffect(() => {
    if (!enabled) return;
    const timer = window.setTimeout(() => void load("", false), delayMs);
    return () => {
      window.clearTimeout(timer);
      request.current?.abort();
    };
  }, [delayMs, enabled, load, reloadKey]);

  const current = list.key === key ? list : null;
  const nextPageToken = current?.nextPageToken ?? "";
  return {
    items: current?.items ?? NO_ITEMS,
    nextPageToken,
    loaded: current?.loaded ?? false,
    loading: status.key === key && status.loading,
    error: status.key === key ? status.error : "",
    refresh: () => load("", false),
    loadMore: () => (nextPageToken && !request.current ? load(nextPageToken, true) : Promise.resolve()),
  };
}
