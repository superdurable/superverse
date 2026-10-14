// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useState } from "react";
import { problemDetailsText, REMEDIES, type Problem, type Remedy } from "@/lib/shared/problem/problem";
import { Button, buttonStyle } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

/** Where a remedy's button leads on the page that shows the problem: a link or an action. */
type RemedyAction = { href: string } | { onClick: () => void; disabled?: boolean };

/** The actions a page offers for the remedies it can carry out; the operator remedy needs none. */
export type RemedyActions = Partial<Record<Remedy, RemedyAction>>;

/** The one button of the remedy: the page's action, copying the details for the operator, or the hint alone. */
function RemedyButton({ problem, actions }: { problem: Problem; actions: RemedyActions }) {
  const [copied, setCopied] = useState(false);
  const { label, hint } = REMEDIES[problem.remedy];
  const action = actions[problem.remedy];
  if (action && "href" in action)
    return (
      <Link href={action.href} className={buttonStyle("accent", "sm")}>
        {label}
      </Link>
    );
  if (action)
    return (
      <Button variant="accent" size="sm" disabled={action.disabled} onClick={action.onClick}>
        {label}
      </Button>
    );
  if (problem.remedy === "FAILURE_REMEDY_CONTACT_OPERATOR")
    return (
      <Button
        variant="accent"
        size="sm"
        onClick={() =>
          void navigator.clipboard.writeText(problemDetailsText(problem)).then(
            () => setCopied(true),
            () => setCopied(false),
          )
        }
      >
        {copied ? "Copied" : label}
      </Button>
    );
  return <p className="text-ink-dim">{hint}</p>;
}

/**
 * A problem in the one format Studio shows everywhere: what happened, why, one button that fixes it
 * with the configuration it lacks, and the technical details folded. It never shows a bare code or
 * a commit hash outside the details.
 */
export function ProblemPanel({
  problem,
  actions = {},
  className,
}: {
  problem: Problem;
  actions?: RemedyActions;
  className?: string;
}) {
  const { code, phase, cause, commit, at } = problem.details;
  const configuration = problem.remedy === "FAILURE_REMEDY_OPEN_SETUP";
  return (
    <section
      role="alert"
      aria-label={problem.title}
      data-testid="problem-panel"
      data-remedy={problem.remedy}
      className={cx(
        "grid gap-1.5 rounded-lg border px-3 py-2.5 text-[13px]",
        configuration ? "border-busy-rule bg-busy-subtle" : "border-fail-rule bg-fail-subtle",
        className,
      )}
    >
      <p className={cx("font-semibold", configuration ? "text-ink" : "text-fail")} data-testid="problem-what">
        {problem.title}
      </p>
      {problem.version && <p className="text-ink-dim">{problem.version}</p>}
      <p className="wrap-anywhere text-ink-dim" data-testid="problem-why">
        {problem.why}
      </p>
      {problem.gaps.length > 0 && (
        <ul className="list-disc pl-5 text-ink-dim" aria-label="Missing configuration">
          {problem.gaps.map((gap) => (
            <li key={gap} className="wrap-anywhere">
              {gap}
            </li>
          ))}
        </ul>
      )}
      <div data-testid="problem-fix">
        <RemedyButton problem={problem} actions={actions} />
      </div>
      <details className="text-[11px] text-ink-faint" data-testid="problem-details">
        <summary className="cursor-pointer">Technical details</summary>
        <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-2 font-mono">
          {code && <Detail name="Code" value={code} />}
          {phase && <Detail name="Phase" value={phase} />}
          {commit && <Detail name="Commit" value={commit} />}
          {at && <Detail name="Time" value={at} />}
          {cause && <Detail name="Cause" value={cause} />}
        </dl>
      </details>
    </section>
  );
}

function Detail({ name, value }: { name: string; value: string }) {
  return (
    <>
      <dt>{name}</dt>
      <dd className="wrap-anywhere whitespace-pre-wrap">{value}</dd>
    </>
  );
}
