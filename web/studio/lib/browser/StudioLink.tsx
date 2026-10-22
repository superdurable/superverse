// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { useEffect, type ComponentProps } from "react";

// One browser-local navigation intent, replaced by the next navigation and cleared at commit.
type NavigationMode = "push" | "replace";
let pending: { from: string; target: URL; mode: NavigationMode } | null = null;

/** Records a client navigation before Next starts it, including programmatic pushes. */
export function beginNavigation(href: string, mode: NavigationMode = "push") {
  pending = { from: window.location.href, target: new URL(href, window.location.href), mode };
}

/** Clears committed or superseded navigation intent; no timer or retained navigation history. */
export function useNavigationCommit() {
  const pathname = usePathname();
  const search = useSearchParams();
  useEffect(() => {
    if (pending && window.location.href !== pending.from) pending = null;
  }, [pathname, search]);
}

/** The latest destination on this page, or null when this render belongs to a page being left. */
export function queryUpdateTarget(pathname: string): { url: URL; mode: NavigationMode } | null {
  const current = new URL(window.location.href);
  if (pending && current.href !== pending.from) pending = null;
  const target = pending?.target ?? current;
  return target.pathname === pathname ? { url: target, mode: pending?.mode ?? "replace" } : null;
}

/** A Studio link records only actual client navigation, excluding modified clicks and external links. */
export function StudioLink({
  href,
  ...props
}: Omit<ComponentProps<typeof Link>, "href" | "onNavigate"> & { href: string }) {
  useNavigationCommit();
  return <Link {...props} href={href} onNavigate={() => beginNavigation(href)} />;
}
