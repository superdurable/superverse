// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import { loadRunHistory } from "@/lib/api/flow/flow-diagnostics";
import { stepContext } from "@/lib/api/flow/flow-history";
import type { FlowHistoryEvent, FlowSummary } from "@/lib/api/flow/flow-types";
import { errorMessage } from "@/lib/api/transport/api-error";
import { useFlowApi } from "@/lib/flow/FlowApiProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { TimeTravelDialog } from "@/components/flow/diagnostics/TimeTravelDialog";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";

/**
 * The full Time travel from a Step, for those who build the Project: it reads the run's history and
 * opens the dialog on the Step's last execution. Hosts keep it behind More details.
 */
export function StepTimeTravel({
  flowId,
  summary,
  stepType,
}: {
  flowId: string;
  summary: FlowSummary;
  stepType: string;
}) {
  const client = useFlowApi();
  const { canIntervene } = useFlowHost();
  const [history, setHistory] = useState<{ events: FlowHistoryEvent[]; event: FlowHistoryEvent | null } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const timeTravel = async () => {
    setLoading(true);
    setError("");
    try {
      const { events } = await loadRunHistory(client, flowId, summary.runId);
      const ofStep = events.filter((event) => stepContext(event).stepType === stepType);
      setHistory({ events, event: ofStep[ofStep.length - 1] ?? null });
    } catch (reason) {
      setError(errorMessage(reason, "The run's history did not load"));
    } finally {
      setLoading(false);
    }
  };

  if (!canIntervene) return null;
  return (
    <>
      <Button size="sm" disabled={loading} onClick={() => void timeTravel()}>
        <Glyph name="chevronLeft" className="size-3.5" />
        {loading ? "Reading history…" : "Time travel"}
      </Button>
      {error && (
        <p role="alert" className="mt-1.5 text-caption text-fail">
          {error}
        </p>
      )}
      {history && (
        <TimeTravelDialog
          summary={summary}
          events={history.events}
          initialEvent={history.event}
          onClose={() => setHistory(null)}
        />
      )}
    </>
  );
}
