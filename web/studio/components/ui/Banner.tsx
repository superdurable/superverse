// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ComponentProps } from "react";
import { cx } from "./cx";

/** fail reports what went wrong; busy asks a person to act. */
export type BannerTone = "fail" | "busy";

const TONE = {
  fail: cx("border-fail-rule bg-fail-subtle text-fail"),
  busy: cx("border-busy-rule bg-busy-subtle text-ink-dim"),
} satisfies Record<BannerTone, string>;

/** A message band above or inside a view. The caller sets its role (alert or status) and margins. */
export function Banner({ tone = "fail", className, ...props }: ComponentProps<"div"> & { tone?: BannerTone }) {
  return <div className={cx("rounded-lg border px-3 py-2.5 text-[13px]", TONE[tone], className)} {...props} />;
}
