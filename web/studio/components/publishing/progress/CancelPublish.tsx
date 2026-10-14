// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef } from "react";
import { cancelDeployment } from "@/lib/api/deployment";
import { cancelReleaseBuild } from "@/lib/api/release";
import { ApiError, newRequestId } from "@/lib/api/transport/api-error";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { cancelTargetOf, type PublishFacts } from "@/lib/project/publishing/publish-stages";
import { errorProblem } from "@/lib/shared/problem/problem";
import { deployingRefusalDeployment } from "@/lib/shared/problem/refusals";
import { Button } from "@/components/ui/Button";
import { PopoverCard, usePopover } from "@/components/ui/Popover";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";

/** The refusal of a deployment that is live or going live. */
const NOT_CANCELLABLE = "DEPLOYMENT_NOT_CANCELLABLE";

/**
 * Cancel and its short confirmation: the running Live is untouched. It cancels the build until its
 * artifacts commit; a build whose deployment may be admitted is refused with the deployment named, which it
 * cancels instead; a deployment until it commits the new Live. Each target keeps one request ID, so a
 * repeated click is the same cancellation. Disabled once the health check passed, because the Flow then
 * commits the new Live.
 */
export function CancelPublish({
  projectId,
  releaseId,
  facts,
  version,
  liveVersion,
}: {
  projectId: string;
  releaseId: string;
  facts: PublishFacts;
  /** What is being published, such as "V13". */
  version: string;
  /** What Live runs now, such as "V12", or empty before the first publish. */
  liveVersion: string;
}) {
  const anchor = useRef<HTMLSpanElement>(null);
  const confirm = usePopover(anchor);
  const action = useSingleFlight<"cancel">();
  const requests = useRef(new Map<string, string>());
  const requestOf = (target: string) => {
    const known = requests.current.get(target);
    if (known) return known;
    const created = newRequestId();
    requests.current.set(target, created);
    return created;
  };
  const target = cancelTargetOf(facts);
  const cancelling = facts.build?.cancellation || facts.deployment?.status === "DEPLOYMENT_STATUS_CANCELLING";
  if (target === null && !cancelling) return null;

  const stopDeployment = (deploymentId: string, signal: AbortSignal) =>
    cancelDeployment({ projectId, deploymentId, requestId: requestOf(`deployment:${deploymentId}`) }, signal);
  const cancel = () => {
    confirm.close();
    void action.run("cancel", async (signal) => {
      if (target === null || target === "too-late") return;
      if (target.kind === "deployment") return stopDeployment(target.deploymentId, signal);
      const buildRevisionId = facts.build?.buildRevisionId ?? "";
      try {
        await cancelReleaseBuild(
          { projectId, releaseId, buildRevisionId, requestId: requestOf(`build:${buildRevisionId}`) },
          signal,
        );
      } catch (reason) {
        const deploymentId = reason instanceof ApiError ? deployingRefusalDeployment(reason.errorInfo) : "";
        if (!deploymentId) throw reason;
        await stopDeployment(deploymentId, signal);
      }
    });
  };
  const late =
    target === "too-late" ||
    (action.failure?.reason instanceof ApiError && action.failure.reason.errorInfo?.reason === NOT_CANCELLABLE);
  const live = liveVersion ? `Live keeps running ${liveVersion}` : "Live stays unpublished";
  return (
    <>
      <span ref={anchor} className="inline-flex">
        <Button
          size="sm"
          disabled={Boolean(cancelling) || late || action.busy !== null}
          title={late ? `Too late to cancel: Live switches to ${version} now` : undefined}
          aria-haspopup="dialog"
          aria-expanded={confirm.open}
          onClick={confirm.toggle}
          data-testid="cancel-publish"
        >
          {cancelling || action.busy ? "Cancelling…" : "Cancel"}
        </Button>
      </span>
      {confirm.open && (
        <PopoverCard anchorRef={anchor} label="Cancel publishing" className="w-[300px] max-w-[calc(100vw-16px)]">
          <div className="grid gap-2 p-1.5 text-[12.5px]" data-testid="cancel-publish-confirm">
            <p className="font-semibold text-ink">Stop publishing {version}?</p>
            <p className="text-ink-dim">{live}; nothing there changes. What was built so far is cleaned up.</p>
            <div className="flex justify-end gap-2">
              <Button size="sm" onClick={confirm.close}>
                Keep publishing
              </Button>
              <Button size="sm" variant="strong" onClick={cancel}>
                Stop publishing
              </Button>
            </div>
          </div>
        </PopoverCard>
      )}
      {late && action.failure && (
        <span className="text-[12.5px] text-ink-dim" role="status">
          Too late to cancel: Live already runs {version}.
        </span>
      )}
      {action.failure && !late && (
        <ProblemPanel
          className="basis-full"
          problem={errorProblem(action.failure.reason, { operation: "Cancelling the publish" })}
          actions={{ FAILURE_REMEDY_APPLY_AGAIN: { onClick: cancel } }}
        />
      )}
    </>
  );
}
