// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { cx } from "@/components/ui/cx";

/**
 * An app's logo: the SVG its release publishes, shown as an image so nothing in it runs, or the first letters of
 * its name on a quiet tile when the release publishes none.
 */
export function AppLogo({ name, svg, className }: { name: string; svg: string; className?: string }) {
  const tile = cx("size-8 shrink-0 rounded-lg", className);
  if (svg)
    return (
      // A data URI keeps the logo out of the network and the page's markup alike.
      // eslint-disable-next-line @next/next/no-img-element
      <img alt="" className={tile} src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`} />
    );
  const letters = name
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0]?.toUpperCase())
    .join("");
  return (
    <span
      aria-hidden="true"
      className={cx(
        tile,
        "grid place-items-center border border-line-soft bg-raised text-[11px] font-semibold text-ink-dim",
      )}
    >
      {letters || "?"}
    </span>
  );
}
