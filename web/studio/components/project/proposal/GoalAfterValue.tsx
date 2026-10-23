// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import type { GoalMovement } from "@/lib/project/goals/proposal-backtest";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { MOVEMENT_WORDS } from "./proposal-words";

/**
 * A goal's value after the change, as both proposal cards show it: green with a check when the goal gets better, red
 * with an alert when it gets worse, so neither rests on colour alone, and "(better)" or "(worse)" for screen readers.
 */
export function GoalAfterValue({
  movement,
  title,
  className,
  children,
}: {
  movement: GoalMovement;
  title?: string;
  className?: string;
  children: ReactNode;
}) {
  const moved = MOVEMENT_WORDS[movement ?? "none"];
  return (
    <>
      <span className={cx("whitespace-nowrap", className, moved.tone)} title={title}>
        {children}
        {moved.glyph && <Glyph name={moved.glyph} className="ml-0.5 inline-block size-3.5 align-[-0.2em]" />}
      </span>
      {moved.words && <span className="sr-only"> ({moved.words.toLowerCase()})</span>}
    </>
  );
}
