// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ComponentProps } from "react";
import { cx } from "./cx";

/** The small uppercase kicker above a heading. The caller sets its margins. */
export function Eyebrow({ className, ...props }: ComponentProps<"p">) {
  return (
    <p className={cx("text-[11px] font-semibold tracking-wider text-ink-faint uppercase", className)} {...props} />
  );
}
