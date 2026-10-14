// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { usePathname } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { CommandPalette } from "@/components/shell/palette/CommandPalette";
import { Sidebar } from "./Sidebar";

function projectIdFromPath(pathname: string): string | undefined {
  const match = /^\/p\/([^/]+)/.exec(pathname);
  if (!match?.[1]) return undefined;
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return match[1];
  }
}

/**
 * The signed-in frame of every page: the sidebar, then the page. A Project page draws its own
 * Builder frame beside it; the account's pages put a PageBar on top.
 */
export function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const activeProjectId = projectIdFromPath(pathname);
  const frameRef = useRef<HTMLDivElement>(null);
  const [jumping, setJumping] = useState(false);

  // ⌘K or Ctrl K opens Jump to from anywhere, and closes it again.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k" && !event.altKey) {
        event.preventDefault();
        setJumping((open) => !open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div ref={frameRef} className="flex h-full min-h-0">
      {/* Inside a Project the sidebar keeps a width of its own, so it remounts on the way in and out. */}
      <Sidebar
        key={activeProjectId ? "project" : "account"}
        activeProjectId={activeProjectId}
        measureRef={frameRef}
        pathname={pathname}
        onJump={() => setJumping(true)}
      />
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>
      {jumping && <CommandPalette onClose={() => setJumping(false)} />}
    </div>
  );
}
