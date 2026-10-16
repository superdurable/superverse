// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import dynamic from "next/dynamic";
import { useMemo, type ReactNode } from "react";
import { setupItems } from "@/lib/project/setup/configuration-items";
import { usePreviewDraft } from "@/lib/project/setup/PreviewDraftProvider";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { flowApiRoutes, studioRoutes } from "@/lib/shared/studio-routes";

const LazyTryIt = dynamic(() => import("@/components/flow/run/tryit/TryIt").then((m) => m.TryIt), {
  ssr: false,
  loading: () => (
    <p className="text-caption text-ink-faint" role="status">
      Loading…
    </p>
  ),
});

/**
 * Try it, for Runs on Preview before its version has a run: Preview's Setup items as the frame's draft reads
 * them, and `autoApply`, Setup's automatic apply, which the caller mounts from `components/project/setup`.
 */
export function TryItSurface({ projectId, autoApply }: { projectId: string; autoApply?: ReactNode }) {
  const configuration = usePreviewDraft()?.configuration ?? null;
  const session = useSessionResource()?.resource;
  const items = useMemo(() => (configuration ? setupItems(configuration) : null), [configuration]);
  const preview = session?.preview;
  return (
    <LazyTryIt
      configurationPath={flowApiRoutes.previewConfiguration(projectId)}
      items={items}
      version={JSON.stringify([
        session?.headCommit,
        preview?.preparedSource?.requestId,
        preview?.preparedSource?.status,
        preview?.appliedConfiguration?.configurationRevision ?? "",
      ])}
      address={preview?.url ?? ""}
      setupHref={studioRoutes.setup(projectId, "preview")}
      autoApply={autoApply}
    />
  );
}
