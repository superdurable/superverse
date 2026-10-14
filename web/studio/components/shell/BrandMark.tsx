// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import Link from "next/link";

import { DexMark } from "./DexMark";

/**
 * Dex leads, Super Durable is the maker credit. Stacking lets Dex be set larger
 * than the company without the lockup growing wider than the mark is tall.
 * Same two-line lockup and type scale as dex-web.
 */
export function BrandMark({ href }: { href?: string }) {
  const mark = (
    <span className="flex min-w-0 items-center gap-[11px]">
      <span className="flex h-7 w-7 shrink-0 items-center justify-center">
        <DexMark size={28} />
      </span>
      <span className="flex min-w-0 flex-col justify-center gap-px leading-none">
        <b className="truncate text-[19px] font-bold tracking-[-0.02em] text-ink">Dex</b>
        <span className="truncate text-[11px] font-semibold tracking-[0.115em] text-ink-faint uppercase">
          Super Durable
        </span>
      </span>
    </span>
  );

  if (href === undefined) return mark;
  return (
    <Link href={href} className="min-w-0">
      {mark}
    </Link>
  );
}
