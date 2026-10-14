// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import { releaseUnusedConnections, type ConnectionsResponse } from "@/lib/api/flow/connector";
import { errorMessage } from "@/lib/api/transport/api-error";
import { createFlowApiClient } from "@/lib/api/transport/flow-api-client";
import { Button } from "@/components/ui/Button";

/**
 * Lets go of the TeamSpace connections that versions before the current Live one used, so the TeamSpace can
 * delete them; Go keeps those the current Live version selects and refuses while a deployment runs.
 */
export function ReleaseUnused({ path, catalog }: { path: string; catalog: ConnectionsResponse }) {
  const [state, setState] = useState<{ busy: boolean; text: string; failed: boolean }>({
    busy: false,
    text: "",
    failed: false,
  });
  const run = async () => {
    setState({ busy: true, text: "", failed: false });
    const client = createFlowApiClient(path, {});
    try {
      await releaseUnusedConnections(client, catalog);
      setState({ busy: false, text: "Only the current Live version's connections stay in use.", failed: false });
    } catch (reason) {
      setState({ busy: false, text: errorMessage(reason, "The connections were not released"), failed: true });
    } finally {
      client.dispose();
    }
  };
  return (
    <div className="mt-3 flex flex-wrap items-center gap-3 text-xs text-ink-faint">
      <Button size="sm" disabled={state.busy} onClick={() => void run()}>
        Release connections older Live versions used
      </Button>
      {state.text && (
        <span className={state.failed ? "text-fail" : undefined} role={state.failed ? "alert" : "status"}>
          {state.text}
        </span>
      )}
    </div>
  );
}
