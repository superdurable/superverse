// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useMemo } from "react";
import { beginNavigation, queryUpdateTarget, useNavigationCommit } from "./StudioLink";
import { withQueryChanges, type QueryChange } from "@/lib/shared/studio-routes";

/**
 * Edits the current URL's query: `queryHref(change)` is the current page with its query changed,
 * for a link, and `replaceQuery(change)` moves there without scrolling or adding history, as in
 * `replaceQuery((query) => query.set("flowType", type))`.
 */
export function useQueryUpdate() {
  const pathname = usePathname();
  const router = useRouter();
  const search = useSearchParams();
  useNavigationCommit();
  return useMemo(() => {
    const queryHref = (change: QueryChange) => withQueryChanges(pathname, search, change);
    const replaceQuery = (change: QueryChange) => {
      const target = queryUpdateTarget(pathname);
      if (!target) return;
      const { url, mode } = target;
      const href = withQueryChanges(url.pathname, url.searchParams, change) + url.hash;
      if (href === url.pathname + url.search + url.hash) return;
      beginNavigation(href, mode);
      router[mode](href, { scroll: false });
    };
    return { queryHref, replaceQuery };
  }, [pathname, router, search]);
}
