// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import { isOpenFlowStatusCode } from "@/lib/api/flow/flow-status";
import type { FlowSummary } from "@/lib/api/flow/flow-types";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { processName } from "@/lib/shared/format";
import type { RunFailure } from "@/components/flow/canvas/run-failure";
import { FlowLink } from "@/components/flow/frame/FlowLink";
import { failureLine } from "@/components/flow/run/failure/failure-copy";
import { Button, buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { RetryStepDialog } from "./RetryStepDialog";
import { StepTimeTravel } from "./StepTimeTravel";

/**
 * What the reader may do at the clicked Step of a run: open its Inbox item while a person must act and
 * hand the Step to the Project's chat. For a failure, in this order: ask the agent with the error
 * written in, retry the failed Step (for builders), and have the agent fix it, on Preview for a Live
 * failure and in place for a test run. Rerunning an open run from the Step (Time travel, for builders)
 * waits behind More details; a failure's own More details holds it instead. Stop is in the run's header.
 */
export function StepActions({
  flowType,
  flowId,
  summary,
  stepType,
  waitsForPerson,
  failure,
  inline = false,
  onRetried,
}: {
  flowType: string;
  flowId: string;
  summary: FlowSummary | null;
  stepType: string;
  /** The run waits here for a person's Action, so its item is in the Inbox. */
  waitsForPerson: boolean;
  /** The failure the drawer leads with, whose Step the actions then concern; null when none. */
  failure: RunFailure | null;
  /** Inside another band, such as the failure's, the actions take no band of their own. */
  inline?: boolean;
  /** A retry started the failed Step again, so the run should be read again. */
  onRetried: () => void;
}) {
  const host = useFlowHost();
  const [retryOpen, setRetryOpen] = useState(false);
  const onLive = host.environment === "live";
  const target = failure?.stepType ?? stepType;
  const about = `${onLive ? "Live run" : "Test run"} ${flowId} ▸ ${processName(flowType)} ▸ ${target}`;
  const ask = (text: string) => host.askAgent?.(about, text);

  return (
    <div
      className={cx("flex flex-wrap gap-1.5 empty:hidden", !inline && "border-b border-line-faint px-4 py-2.5")}
      data-testid="step-actions"
    >
      {waitsForPerson && (
        <FlowLink className={buttonStyle("secondary", "sm")} to={{ surface: "inbox", flowType, flowId }}>
          <Glyph name="inbox" className="size-3.5" />
          Open in Inbox
        </FlowLink>
      )}
      {host.askAgent && (
        <Button
          size="sm"
          onClick={() =>
            failure ? ask(`${failureLine(failure)} Why did this happen, and what should I do?`) : host.askAgent?.(about)
          }
        >
          <Glyph name="spark" className="size-3.5" />
          Ask the agent
        </Button>
      )}
      {failure && host.canIntervene && summary && (
        <Button size="sm" onClick={() => setRetryOpen(true)}>
          <Glyph name="refresh" className="size-3.5" />
          Retry this step
        </Button>
      )}
      {failure && onLive && host.askAgent && (
        <Button
          size="sm"
          variant="primary"
          onClick={() =>
            ask(`This failed on Live: ${failureLine(failure)} Fix it on Preview so it doesn't happen again.`)
          }
        >
          <Glyph name="spark" className="size-3.5" />
          Fix on Preview
        </Button>
      )}
      {failure && host.environment === "preview" && host.askAgent && (
        <Button
          size="sm"
          variant="primary"
          onClick={() => ask(`This failed in a test run: ${failureLine(failure)} Fix it so it doesn't happen again.`)}
        >
          <Glyph name="spark" className="size-3.5" />
          Fix with the agent
        </Button>
      )}
      {!inline && host.canIntervene && summary && isOpenFlowStatusCode(summary.flowStatusCode) && (
        <MoreDetails surface="runs.step-tools" className="w-full">
          <StepTimeTravel flowId={flowId} summary={summary} stepType={stepType} />
        </MoreDetails>
      )}
      {summary && failure && retryOpen && (
        <RetryStepDialog
          summary={summary}
          failure={failure}
          onClose={() => setRetryOpen(false)}
          onRetried={onRetried}
        />
      )}
    </div>
  );
}
