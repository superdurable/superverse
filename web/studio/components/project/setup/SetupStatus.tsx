// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import type { PreviewSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewSnapshot";

/** Why Setup's table is not shown yet: the latest push is building, failed to build, or is not built. */
export function UnbuiltStatus({ preview }: { preview: PreviewSnapshot__Output | null }) {
  const source = preview?.preparedSource;
  return (
    <p className="text-[13px] text-ink-faint" role="status" data-testid="preview-status">
      {source?.status === "PREVIEW_SOURCE_STATUS_PREPARING"
        ? "Building the latest push… Setup opens when it is ready."
        : source?.status === "PREVIEW_SOURCE_STATUS_FAILED"
          ? "The latest push did not build; Build processes below says why."
          : "The latest push is not built yet; Build processes below builds it."}
    </p>
  );
}
