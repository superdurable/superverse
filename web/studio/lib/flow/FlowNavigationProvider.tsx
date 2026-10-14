// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { createContext, useContext, useMemo, type ReactNode } from "react";
import { flowTargetHref, type FlowTarget } from "@/lib/shared/studio-routes";

/**
 * How Flow views move between Runs, Inbox, Diagnostics and Configuration. FlowScope supplies it from
 * the Studio URL, so Flow views neither build Studio URLs nor read the router.
 */
interface FlowNavigation {
  /** The Flow selection carried in the current URL; `diagnostics` opens the selected run's diagnostics. */
  params: { flowType: string; flowId: string; runId: string; diagnostics: boolean };
  /** The current URL, which a redirect compares with its destination. */
  currentHref: string;
  href(target: FlowTarget): string;
  /** Moves to `target`; `replace` replaces the current history entry. */
  navigate(target: FlowTarget, options?: { replace?: boolean }): void;
}

const FlowNavigationContext = createContext<FlowNavigation | null>(null);

export function FlowNavigationProvider({ children }: { children: ReactNode }) {
  const router = useRouter();
  const pathname = usePathname();
  const search = useSearchParams();
  const navigation = useMemo<FlowNavigation>(
    () => ({
      params: {
        flowType: search.get("flowType") ?? "",
        flowId: search.get("flowId") ?? "",
        runId: search.get("runId") ?? "",
        diagnostics: search.get("diagnostics") === "true",
      },
      currentHref: `${pathname}${search.size > 0 ? `?${search}` : ""}`,
      href: (target) => flowTargetHref(target, pathname, search),
      navigate: (target, options) => {
        const href = flowTargetHref(target, pathname, search);
        if (options?.replace) router.replace(href);
        else router.push(href);
      },
    }),
    [pathname, router, search],
  );
  return <FlowNavigationContext.Provider value={navigation}>{children}</FlowNavigationContext.Provider>;
}

export function useFlowNavigation(): FlowNavigation {
  const navigation = useContext(FlowNavigationContext);
  if (!navigation) throw new Error("Flow views require a FlowScope");
  return navigation;
}

/** The Flow selection carried in the current URL. */
export function useFlowRouteParams(): FlowNavigation["params"] {
  return useFlowNavigation().params;
}
