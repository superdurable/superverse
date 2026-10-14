// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ComponentProps } from "react";
import { cx } from "./cx";

/**
 * A small toggle in a row of choices, such as a tab of Build's step switch or a canvas setting. The
 * caller supplies the role and its aria state (tab, radio); `on` draws the chosen one.
 */
export function Chip({ on, className, ...props }: Omit<ComponentProps<"button">, "type"> & { on: boolean }) {
  return (
    <button
      type="button"
      data-on={on ? "true" : "false"}
      className={cx(
        "rounded-md border border-line-soft bg-panel px-2 py-0.5 text-sm whitespace-nowrap text-ink-dim transition-colors hover:border-mark hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent data-[on=true]:border-ink-faint data-[on=true]:bg-raised data-[on=true]:text-ink",
        className,
      )}
      {...props}
    />
  );
}
