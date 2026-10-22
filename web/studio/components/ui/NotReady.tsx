// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";
import { buttonStyle } from "./Button";
import { cx } from "./cx";

/*
 * Four different things get conflated into "no data", and the confusion is
 * expensive in both directions: a placeholder that reads as a bug sends people to
 * support, and an error that reads as an empty state gets ignored.
 *
 *   placeholder  the surface is not built yet
 *   empty        built, nothing in it yet
 *   unavailable  built, but the source cannot serve it
 *   error        something failed
 *
 * So the register is fixed by the kind and is not the caller's to choose. Form
 * carries it as well as hue -- dashed for a stand-in, a square for failure --
 * because the state family collapses to dE 5.3 under both dichromat simulations.
 */
export type NotReadyKind = "placeholder" | "empty" | "unavailable" | "error";

const REGISTER = {
  placeholder: { frame: cx("border-dashed border-line-soft bg-panel"), mark: null },
  empty: { frame: cx("border-line-soft bg-panel"), mark: null },
  unavailable: { frame: cx("border-busy-rule bg-busy-subtle"), mark: cx("rounded-full bg-busy") },
  // Square, not round: the only shape in the product that means failed.
  error: { frame: cx("border-fail-rule bg-fail-subtle"), mark: cx("rounded-[1px] bg-fail") },
} satisfies Record<NotReadyKind, { frame: string; mark: string | null }>;

export function NotReady({
  kind,
  heading,
  children,
  action,
  retry = false,
  detail,
  loader,
}: {
  kind: NotReadyKind;
  heading: string;
  children: ReactNode;
  /** The one way forward: a link, or a button that runs `onClick`. */
  action?: { label: string; href: string } | { label: string; onClick: () => void };
  /** Offers Try again, which reads the page again. */
  retry?: boolean;
  /* An identifier a person can quote when reporting. Errors carry one; the calm
     kinds have nothing to quote. */
  detail?: string;
  /** Work is under way: the card is headed by this, the running mark of `ui/wait`, and its heading is announced. */
  loader?: ReactNode;
}) {
  const register = REGISTER[kind];
  const said = useSaidAfterMount(loader ? heading : "");
  return (
    <div className="box-border flex min-h-0 w-full min-w-0 flex-1 items-center justify-center overflow-y-auto p-4 sm:p-8">
      <div className={cx("w-full max-w-md min-w-0 rounded-lg border p-6", register.frame)} data-not-ready={kind}>
        {/* Only the heading is the status, not the whole card with its instructions. */}
        <p aria-live="polite" className="sr-only" role="status">
          {said}
        </p>
        {loader && <div className="mb-4">{loader}</div>}
        <div className="flex items-center gap-2">
          {register.mark && <span aria-hidden="true" className={cx("size-2 shrink-0", register.mark)} />}
          <h2 className="text-[17px] font-semibold text-ink">{heading}</h2>
        </div>
        <p className="mt-2 text-[15px] leading-relaxed text-ink-dim">{children}</p>
        {detail && <p className="mt-3 font-mono text-[13px] break-all text-ink-faint">{detail}</p>}
        {action &&
          ("href" in action ? (
            <Link href={action.href} className={cx(buttonStyle("accent"), "mt-4")}>
              {action.label}
            </Link>
          ) : (
            <button type="button" className={cx(buttonStyle("accent"), "mt-4")} onClick={action.onClick}>
              {action.label}
            </button>
          ))}
        {retry && (
          <button
            type="button"
            className={cx(buttonStyle("secondary"), "mt-4")}
            onClick={() => window.location.reload()}
          >
            Try again
          </button>
        )}
      </div>
    </div>
  );
}

/**
 * The status words, set only after the region is in the page: a live region inserted already holding its words,
 * or turned on around them, is often not announced, while a change inside an existing region is.
 */
function useSaidAfterMount(words: string): string {
  const [said, setSaid] = useState("");
  useEffect(() => {
    const timer = window.setTimeout(() => setSaid(words), 0);
    return () => window.clearTimeout(timer);
  }, [words]);
  return said;
}
