// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";

/** The 48px bar on top of an account page: its title, and whatever the page puts at the right. */
export function PageBar({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <header className="flex h-header shrink-0 items-center gap-3 border-b border-line-soft bg-panel px-3.5">
      <h1 className="min-w-0 truncate text-[15px] font-semibold text-ink">{title}</h1>
      {children && <div className="ml-auto flex items-center gap-2">{children}</div>}
    </header>
  );
}
