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
  return useMemo(() => {
    const queryHref = (change: QueryChange) => withQueryChanges(pathname, search, change);
    const replaceQuery = (change: QueryChange) => router.replace(queryHref(change), { scroll: false });
    return { queryHref, replaceQuery };
  }, [pathname, router, search]);
}
