// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { FlowApiProvider } from "@/lib/flow/FlowApiProvider";
import { LazyLiveIdentities } from "./LazyFlowViews";

/**
 * The connections and channels a Live draft (`basePath`) will run with once published, read from its
 * configuration host; `version` is the draft revision on screen.
 */
export function LiveIdentitiesSurface({ basePath, version }: { basePath: string; version: string }) {
  return (
    <FlowApiProvider key={basePath} basePath={basePath}>
      <LazyLiveIdentities version={version} />
    </FlowApiProvider>
  );
}
