// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ComponentProps } from "react";
import { cx } from "@/components/ui/cx";

/**
 * The tones a badge may take, each with the meaning the palette licenses: neutral says nothing,
 * accent is in progress, busy means a person must act, pass is done and fail is failed.
 */
export type BadgeTone = "neutral" | "accent" | "busy" | "pass" | "fail";

const TONE = {
  neutral: cx("border-line-soft bg-panel text-ink-dim"),
  accent: cx("border-accent-rule bg-accent-subtle text-accent"),
  busy: cx("border-busy-rule bg-busy-subtle text-busy"),
  pass: cx("border-pass-rule bg-pass-subtle text-pass"),
  fail: cx("border-fail-rule bg-fail-subtle text-fail"),
} satisfies Record<BadgeTone, string>;

/** The one small pill: a state, a count or a label beside a name. Never a control. */
export function Badge({ tone = "neutral", className, ...props }: ComponentProps<"span"> & { tone?: BadgeTone }) {
  return (
    <span
      className={cx(
        "inline-flex items-center rounded-full border px-1.5 text-caption leading-[18px] font-medium whitespace-nowrap",
        TONE[tone],
        className,
      )}
      {...props}
    />
  );
}
