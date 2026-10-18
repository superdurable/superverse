// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { ACCEPTED_FILES } from "@/lib/session/attachments/attachment-files";
import type { AttachmentTray } from "@/lib/session/attachments/use-attachment-tray";

/**
 * Attach (+): a picker of several files of the accepted types, whose choice joins the tray. While attaching
 * is unavailable, such as while a local agent runs the Session, it is disabled and its tooltip says why.
 */
export function AttachButton({
  tray,
  disabled = false,
  className,
}: {
  tray: AttachmentTray;
  disabled?: boolean;
  className?: string;
}) {
  const input = useRef<HTMLInputElement | null>(null);
  const unavailable = tray.disabledReason !== null;
  const title = tray.disabledReason ?? "Attach files: images, documents or JSON";
  return (
    <>
      <button
        type="button"
        aria-label="Attach files"
        title={title}
        aria-disabled={unavailable || disabled ? "true" : undefined}
        onClick={() => {
          if (unavailable || disabled) return;
          input.current?.click();
        }}
        className={cx(
          "grid size-7 shrink-0 place-items-center rounded-[7px]",
          unavailable || disabled ? "cursor-default text-mark" : "text-ink-faint hover:bg-hover hover:text-ink",
          className,
        )}
      >
        <Glyph name="plus" />
      </button>
      <input
        ref={input}
        type="file"
        multiple
        accept={ACCEPTED_FILES}
        hidden
        aria-hidden="true"
        tabIndex={-1}
        data-testid="attach-files-input"
        onChange={(event) => {
          tray.add([...(event.target.files ?? [])]);
          // The same file may be chosen again after its chip was removed.
          event.target.value = "";
        }}
      />
    </>
  );
}
