// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef, useState, type DragEvent, type ReactNode } from "react";
import { cx } from "@/components/ui/cx";
import type { AttachmentTray } from "@/lib/session/attachments/use-attachment-tray";

function carriesFiles(event: DragEvent): boolean {
  return [...event.dataTransfer.types].includes("Files");
}

/**
 * A region that takes files dropped on it, such as the chat column, into the tray. While files are dragged
 * over it, it says where they go, or why attaching is unavailable.
 */
export function AttachmentDropZone({
  tray,
  className,
  children,
}: {
  tray: AttachmentTray | null;
  className?: string;
  children: ReactNode;
}) {
  const [over, setOver] = useState(false);
  // Entering a child leaves its parent: the zone counts the nested enters so it stays lit until the drag is out.
  const depth = useRef(0);
  if (!tray) return <div className={className}>{children}</div>;
  return (
    <div
      className={cx("relative", className)}
      onDragEnter={(event) => {
        if (!carriesFiles(event)) return;
        event.preventDefault();
        depth.current += 1;
        setOver(true);
      }}
      onDragOver={(event) => {
        if (!carriesFiles(event)) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = tray.disabledReason ? "none" : "copy";
      }}
      onDragLeave={(event) => {
        if (!carriesFiles(event)) return;
        depth.current = Math.max(0, depth.current - 1);
        if (depth.current === 0) setOver(false);
      }}
      onDrop={(event) => {
        if (!carriesFiles(event)) return;
        event.preventDefault();
        depth.current = 0;
        setOver(false);
        tray.add([...event.dataTransfer.files]);
      }}
    >
      {children}
      {over && (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-2 z-20 grid place-items-center rounded-xl border-2 border-dashed border-accent-rule bg-accent-wash text-body text-accent"
        >
          {tray.disabledReason ?? "Drop files to attach them"}
        </div>
      )}
    </div>
  );
}
