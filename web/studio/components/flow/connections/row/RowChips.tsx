// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ConnectionView } from "@/lib/api/flow/connector";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { NARROW_ONLY } from "@/components/flow/connections/model/connection-words";
import {
  connectionItemKey,
  rowTone,
  sideConnection,
  sideItem,
  type SetupSide,
} from "@/components/flow/connections/model/row-model";

const CHIP = {
  ok: cx("border-line-soft bg-band-0 text-ink-dim"),
  warn: cx("border-busy-rule bg-busy-subtle text-busy"),
  none: cx("border-fail-rule bg-fail-subtle text-fail"),
};

const WORDS = { ok: "", warn: " · worth a look", none: " · something to set up" };

/**
 * A row's state on each side, shown instead of its controls once the tool panel is narrower than 780px:
 * Preview and Publish, each a check or a warning glyph, with what the side uses in its hover hint. A
 * click reaches the row, which opens.
 */
export function RowChips({
  sides,
  connection,
  nameOf,
}: {
  sides: (SetupSide | null)[];
  connection: ConnectionView;
  /** A Teamspace connection's name by its ID. */
  nameOf: (connectionId: string) => string;
}) {
  return (
    <span className={cx(NARROW_ONLY, "items-center justify-end gap-1.5")} data-testid="setup-row-chips">
      {sides.map((side, index) => {
        const tone = rowTone(side, connection);
        const own = sideConnection(side, connection);
        const following = sideItem(side, connectionItemKey(connection)).item?.state === "STATE_FOLLOWS_PREVIEW";
        const name = own?.sharedConnectionId ? nameOf(own.sharedConnectionId) : "not connected";
        const which = side?.name === "next" ? "Next publish" : "Preview";
        return (
          <span
            key={side?.name ?? index}
            title={`${which}: ${following ? "same as Preview, " : ""}${name}${WORDS[tone]}`}
            data-tone={tone}
            className={cx(
              "inline-flex h-6 items-center gap-1 rounded-full border pr-2.25 pl-1.75 text-[12px] font-medium whitespace-nowrap",
              CHIP[tone],
            )}
          >
            <Glyph name={tone === "ok" ? "check" : "alert"} className="size-3.25" />
            {side?.name === "next" ? "Publish" : "Preview"}
          </span>
        );
      })}
    </span>
  );
}
