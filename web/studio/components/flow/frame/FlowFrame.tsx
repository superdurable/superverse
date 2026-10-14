// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ComponentProps } from "react";
import { cx } from "@/components/ui/cx";

/** canvas holds a drawing (Runs, Inbox, Connectors, Build's Design); page holds reading (Diagnostics). */
export type FlowPlane = "canvas" | "page";

const PLANE = {
  canvas: cx("bg-canvas text-ink"),
  page: cx("bg-bg text-ink-dim"),
} satisfies Record<FlowPlane, string>;

/**
 * The root of every Flow view: the platform font at 13px on its plane. The element defaults that go
 * with it (heading sizes, disabled controls) key on data-flow-frame in the base layer of
 * app/globals.css, below every rule a view writes. `className` places the frame and carries the
 * view's own root classes.
 */
export function FlowFrame({ plane = "canvas", className, ...props }: ComponentProps<"div"> & { plane?: FlowPlane }) {
  return (
    <div
      data-flow-frame=""
      className={cx("h-full font-system text-[13px]/[1.45]", PLANE[plane], className)}
      {...props}
    />
  );
}
