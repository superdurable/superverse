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
import { isOpenFlowStatusCode } from "@/lib/api/flow/flow-status";
import type { FlowHistoryEvent, FlowSummary } from "@/lib/api/flow/flow-types";
import { errorMessage } from "@/lib/api/transport/api-error";
import { useFlowApi } from "@/lib/flow/FlowApiProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { processName } from "@/lib/shared/format";
import { TimeTravelDialog } from "@/components/flow/diagnostics/TimeTravelDialog";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { Button, buttonStyle } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";

/**
 * What the reader may do at the clicked Step of a run: open its Inbox item while a person must act,
 * rerun the run from this Step (Time travel, for builders, preselecting the Step's last execution),
 * hand the Step to the Project's chat, and, for a Step that failed on Live, ask for the fix on Preview
 * with the error already written. Stop is in the run's header.
 */
export function StepActions({
  flowType,
  flowId,
  summary,
  stepType,
  waitsForPerson,
  failure,
}: {
  flowType: string;
  flowId: string;
  summary: FlowSummary | null;
  stepType: string;
  /** The run waits here for a person's Action, so its item is in the Inbox. */
  waitsForPerson: boolean;
  /** Why the Step failed, when it did. */
  failure: string | null;
}) {
  const host = useFlowHost();
  const client = useFlowApi();
  const [history, setHistory] = useState<{ events: FlowHistoryEvent[]; event: FlowHistoryEvent | null } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const onLive = host.environment === "live";
  const about = `${onLive ? "Live run" : "Test run"} ${flowId} ▸ ${processName(flowType)} ▸ ${stepType}`;

  const timeTravel = async () => {
    if (!summary) return;
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

  return (
    <div className="flex flex-wrap gap-1.5 border-b border-line-faint px-4 py-2.5" data-testid="step-actions">
      {failure && onLive && host.askAgent && (
        <Button
          size="sm"
          variant="primary"
          onClick={() =>
            host.askAgent?.(about, `This failed on Live: ${failure} Fix it on Preview so it doesn't happen again.`)
          }
        >
          <Glyph name="spark" className="size-3.5" />
          Fix on Preview
        </Button>
      )}
      {waitsForPerson && (
        <FlowLink className={buttonStyle("secondary", "sm")} to={{ surface: "inbox", flowType, flowId }}>
          <Glyph name="inbox" className="size-3.5" />
          Open in Inbox
        </FlowLink>
      )}
      {host.canIntervene && summary && isOpenFlowStatusCode(summary.flowStatusCode) && (
        <Button size="sm" disabled={loading} onClick={() => void timeTravel()}>
          <Glyph name="chevronLeft" className="size-3.5" />
          {loading ? "Reading history…" : "Time travel"}
        </Button>
      )}
      {host.askAgent && (
        <Button size="sm" onClick={() => host.askAgent?.(about)}>
          <Glyph name="spark" className="size-3.5" />
          Ask the agent
        </Button>
      )}
      {error && (
        <p role="alert" className="w-full text-xs text-fail">
          {error}
        </p>
      )}
      {summary && history && (
        <TimeTravelDialog
          summary={summary}
          events={history.events}
          initialEvent={history.event}
          onClose={() => setHistory(null)}
        />
      )}
    </div>
  );
}
