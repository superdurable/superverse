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
 * primary is the one brand green (a page's main action), secondary the neutral outline, strong a
 * neutral fill for a form's own action where green already says something else (in Tools, Setup
 * green only means connected), accent an interactive outline where green would read as "done", danger a
 * destructive action and quiet a borderless control such as a dialog's close button.
 */
export type ButtonVariant = "primary" | "secondary" | "strong" | "accent" | "danger" | "quiet";
export type ButtonSize = "sm" | "md" | "icon";

const VARIANT = {
  // Disabled drops the fill rather than fading it: faded ink on a faded green is illegible. Hover is
  // not-disabled rather than enabled, so a link drawn with buttonStyle, never :enabled, hovers too.
  primary: cx(
    "border-cta bg-cta text-cta-on not-disabled:hover:border-cta-hover not-disabled:hover:bg-cta-hover disabled:border-line-soft disabled:bg-raised disabled:text-ink-faint disabled:opacity-100",
  ),
  secondary: cx(
    "border-line-soft bg-panel text-ink-dim not-disabled:hover:border-mark not-disabled:hover:text-ink disabled:opacity-50",
  ),
  strong: cx(
    "border-ink bg-ink text-panel not-disabled:hover:border-ink-dim not-disabled:hover:bg-ink-dim disabled:opacity-50",
  ),
  accent: cx("border-accent-rule bg-transparent text-accent not-disabled:hover:bg-accent-subtle disabled:opacity-50"),
  danger: cx("border-fail-rule bg-transparent text-fail not-disabled:hover:bg-fail-subtle disabled:opacity-50"),
  quiet: cx("border-transparent bg-transparent text-ink-faint not-disabled:hover:text-ink disabled:opacity-50"),
} satisfies Record<ButtonVariant, string>;

const SIZE = {
  sm: cx("rounded-md px-2 py-1 text-xs"),
  md: cx("min-h-9 rounded-lg px-3 py-2 text-[13px] font-semibold"),
  icon: cx("size-8.5 rounded-lg text-[19px] leading-none"),
} satisfies Record<ButtonSize, string>;

/** A button look's classes, for a link that must read as a button. */
export function buttonStyle(variant: ButtonVariant = "secondary", size: ButtonSize = "md"): string {
  return cx(
    "inline-flex items-center justify-center gap-1.5 border whitespace-nowrap transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed",
    VARIANT[variant],
    SIZE[size],
  );
}

/** A button; `type` defaults to "button", so only an explicit submit button submits a form. */
export function Button({
  variant,
  size,
  type = "button",
  className,
  ...props
}: ComponentProps<"button"> & { variant?: ButtonVariant; size?: ButtonSize }) {
  return <button type={type} className={cx(buttonStyle(variant, size), className)} {...props} />;
}
