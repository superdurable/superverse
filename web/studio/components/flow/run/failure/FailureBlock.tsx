// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import type { RunFailure } from "@/components/flow/canvas/run-failure";
import { cx } from "@/components/ui/cx";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { FAILURE_COPY, whatHappened, whatHappensNow } from "./failure-copy";

const HEADING = cx("m-0 text-caption font-semibold");

/**
 * A failure at the top of the run drawer in business terms: what happened, in one sentence from the
 * Step and its error, and what happens now, read from the run. The engine's own words wait behind
 * More details; the actions the reader may take come as `children`. Without a failed Step, the run
 * itself ended as failed and the block says only that.
 */
export function FailureBlock({
  failure,
  flowStatusCode,
  tools,
  children,
}: {
  failure: RunFailure | null;
  flowStatusCode?: number;
  /** Engine tools for the failed Step, such as Time travel, shown with its details. */
  tools?: ReactNode;
  /** The actions the reader may take. */
  children?: ReactNode;
}) {
  const outlook = failure ? whatHappensNow(failure, flowStatusCode) : { text: FAILURE_COPY.runFailedNow, stuck: false };
  const tries = failure && failure.attempts > 1 ? String(failure.attempts) : "";
  return (
    <section
      aria-label={FAILURE_COPY.region}
      className="grid gap-2 border-b border-line-faint bg-fail-subtle/40 px-4 py-3"
      data-stuck={outlook?.stuck ? "true" : undefined}
      data-testid="run-failure"
    >
      <div className="grid gap-0.5">
        <h3 className={cx(HEADING, "text-fail")}>{FAILURE_COPY.happened}</h3>
        <p className="m-0 text-reading text-ink" data-testid="run-failure-what">
          {failure ? whatHappened(failure) : FAILURE_COPY.runFailed}
        </p>
      </div>
      {outlook && (
        <div className="grid gap-0.5">
          <h3 className={cx(HEADING, outlook.stuck ? "text-busy" : "text-ink-faint")}>{FAILURE_COPY.happensNow}</h3>
          <p className="m-0 text-body text-ink-dim" data-testid="run-failure-now">
            {outlook.text}
          </p>
        </div>
      )}
      {children}
      {failure && (
        <MoreDetails surface="runs.failure">
          <dl className="grid grid-cols-[max-content_1fr] gap-x-3 gap-y-1 text-caption">
            {failure.error && <Detail term={FAILURE_COPY.error} value={failure.error} />}
            {failure.errorType && <Detail term={FAILURE_COPY.code} value={failure.errorType} />}
            {tries && <Detail term={FAILURE_COPY.tries} value={tries} />}
            <Detail term={FAILURE_COPY.step} value={failure.stepType} />
          </dl>
          {tools && <div className="mt-2">{tools}</div>}
        </MoreDetails>
      )}
    </section>
  );
}

function Detail({ term, value }: { term: string; value: string }) {
  return (
    <>
      <dt className="text-ink-faint">{term}</dt>
      <dd className="m-0 min-w-0 font-mono wrap-anywhere whitespace-pre-wrap text-ink-dim">{value}</dd>
    </>
  );
}
