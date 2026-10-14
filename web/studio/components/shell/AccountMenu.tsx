// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { logout } from "@/lib/api/account";
import { errorMessage } from "@/lib/api/transport/api-error";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { useAccount } from "@/lib/account/AccountProvider";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { SIDEBAR_ROW } from "./sidebar-styles";

/** The signed-in account and its sign-out, the last row of the sidebar. The menu opens upward. */
export function AccountMenu({ labelClassName }: { labelClassName?: string }) {
  const router = useRouter();
  const account = useAccount();
  const [error, setError] = useState("");

  async function signOut() {
    setError("");
    try {
      await logout();
      router.replace(studioRoutes.login);
      router.refresh();
    } catch (reason) {
      setError(errorMessage(reason, "Sign out failed"));
    }
  }

  return (
    // A details/summary, so the menu opens and closes without React state. One browser family draws the
    // summary marker as a list marker, the other as a pseudo-element; both are hidden.
    <details className="relative" data-testid="account-menu">
      <summary aria-label="Account" className={cx(SIDEBAR_ROW, "list-none [&::-webkit-details-marker]:hidden")}>
        <span
          aria-hidden="true"
          className="-ml-0.5 grid size-5 shrink-0 place-items-center rounded-full bg-ink text-[9px] font-semibold text-panel"
        >
          {account?.email[0]?.toUpperCase() ?? "●"}
        </span>
        <span className={cx("min-w-0 truncate", labelClassName)}>{account?.email ?? "Studio account"}</span>
      </summary>
      <div className="absolute bottom-full left-2 z-40 mb-1 w-60 rounded-[10px] bg-panel p-1.5 shadow-(--shadow-lift)">
        <p className="px-2 py-1.5 text-[13px] break-all text-ink">{account?.email ?? "Studio account"}</p>
        {account?.isAdmin && <p className="px-2 pb-1 text-xs text-ink-faint">Administrator</p>}
        <button
          type="button"
          onClick={() => void signOut()}
          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[13px] text-ink hover:bg-hover focus-visible:shadow-(--focus-ring) focus-visible:outline-none"
        >
          <Glyph name="logout" />
          Sign out
        </button>
        {error && (
          <p role="alert" className="px-2 pt-1 text-xs text-fail">
            {error}
          </p>
        )}
      </div>
    </details>
  );
}
