// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useLayoutEffect, useRef, type ComponentProps, type RefObject } from "react";
import { cx } from "./cx";

export type DialogWidth = "md" | "lg" | "xl";

const WIDTH = {
  md: cx("w-[min(520px,calc(100vw-2rem))]"),
  lg: cx("w-[min(680px,calc(100vw-2rem))]"),
  xl: cx("w-[min(1040px,calc(100vw-2rem))]"),
} satisfies Record<DialogWidth, string>;

/**
 * A native modal dialog, open while it is mounted, so each opening starts from fresh state and the
 * browser keeps focus trapping, Escape and focus restoration. Escape calls `onDismiss`, and the
 * owner decides whether the dialog may close now. The caller pads its content.
 */
export function Dialog({
  ref,
  width = "md",
  onDismiss,
  className,
  ...props
}: Omit<ComponentProps<"dialog">, "ref" | "open" | "onCancel"> & {
  ref?: RefObject<HTMLDialogElement | null>;
  width?: DialogWidth;
  onDismiss: () => void;
}) {
  const own = useRef<HTMLDialogElement | null>(null);
  const dialog = ref ?? own;
  // Close before React removes the dialog so native focus restoration still sees a modal.
  useLayoutEffect(() => {
    const element = dialog.current;
    if (element && !element.open) element.showModal();
    return () => {
      if (element?.open) element.close();
    };
  }, [dialog]);
  return (
    <dialog
      ref={dialog}
      onCancel={(event) => {
        event.preventDefault();
        onDismiss();
      }}
      className={cx(
        "m-auto max-h-[calc(100dvh-2rem)] overflow-auto rounded-xl border border-line-soft bg-panel text-ink shadow-[0_24px_70px_rgba(0,0,0,0.28)] backdrop:bg-black/45",
        WIDTH[width],
        className,
      )}
      {...props}
    />
  );
}
