// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import Link from "next/link";
import type { ReactNode } from "react";
import { focusAnchor } from "@/lib/browser/hash-focus";
import type { EnvironmentStatus, StatusFix, StatusTone } from "@/lib/project/setup/environment-status";
import type { ProjectEnvironment } from "@/lib/shared/studio-routes";
import { cx } from "@/components/ui/cx";

/** The segment around one environment: its name and, when something is off, its word. */
const SEGMENT = cx("inline-flex h-7 min-w-0 items-center rounded-[7px] border text-[13px] whitespace-nowrap");
/**
 * The selected environment's segment, filled with the environment's colour, grey Preview and green Live, in
 * the panel's colour; its healthy dot turns the panel's colour and its word reads on the fill.
 */
const SELECTED = cx("border-env bg-env font-semibold text-panel");
const UNSELECTED = cx("border-transparent text-ink-faint");
const FOCUS = cx("rounded-[6px] focus-visible:shadow-(--focus-ring) focus-visible:outline-none");

/**
 * The mark of each tone: red failed, amber missing or unapplied, a blue pulse while it moves, hollow
 * while stopped or absent. A healthy environment is filled in its own colour.
 */
const DOT: Record<StatusTone, string> = {
  fail: "border-fail bg-fail",
  attention: "border-busy bg-busy",
  pulse: "border-accent bg-accent animate-pulse-soft motion-reduce:animate-none",
  ok: "",
  plain: "border-mark",
};
const HEALTHY_DOT: Record<ProjectEnvironment, string> = {
  preview: "border-ink-dim bg-ink-dim",
  live: "border-pass bg-pass",
};

/** The colours of an environment's status dot, wherever its status is drawn. */
export function statusDotClass(environment: ProjectEnvironment, tone: StatusTone): string {
  return tone === "ok" ? HEALTHY_DOT[environment] : DOT[tone];
}

const WORDS: Record<StatusTone, string> = {
  fail: "text-fail",
  attention: "text-busy",
  pulse: "text-accent",
  ok: "text-ink-faint",
  plain: "text-ink-faint",
};

/** Where each fix leads, as the word's accessible name says it. */
const FIX_WORDS: Record<StatusFix["to"], string> = {
  interface: "open the Interface",
  setup: "open Setup",
  publishing: "open Publishing",
};

/** One environment's status as its tab shows it. */
export type TabStatus = Pick<EnvironmentStatus<string>, "kind" | "tone" | "word" | "fix" | "detail">;

/**
 * An environment of the bar: its one dot and its name, which makes every surface show that
 * environment (a label only without a target, `href` null). A word follows only when something is off,
 * and leads to the fix when `fixHref` names one. The whole status is the hover hint and part of the
 * name's accessible text.
 */
export function EnvironmentStatusTab({
  environment,
  name,
  selected,
  href,
  status,
  title,
  fixHref,
  word,
  testId,
}: {
  environment: ProjectEnvironment;
  name: string;
  selected: boolean;
  href: string | null;
  status: TabStatus | null;
  title?: string;
  /** Where the word leads, or null when it leads nowhere. */
  fixHref: string | null;
  /** A word that acts, drawn in place of the status's own. */
  word?: ReactNode;
  testId: string;
}) {
  const tone = status?.tone ?? "plain";
  const hint = title ?? (status ? `${name}: ${status.detail}` : name);
  const label = (
    <>
      <span
        aria-hidden="true"
        className={cx(
          "size-2 shrink-0 rounded-full border-[1.5px]",
          selected && (tone === "ok" || tone === "plain")
            ? cx("border-panel", tone === "ok" && "bg-panel")
            : statusDotClass(environment, tone),
          selected && tone !== "ok" && tone !== "plain" && "ring-1 ring-panel",
        )}
      />
      {name}
      {status && <span className="sr-only">: {status.detail}</span>}
    </>
  );
  const nameClass = cx(FOCUS, "inline-flex h-full min-w-0 items-center gap-1.5 px-2.25", !selected && "hover:text-ink");
  const marks = { "data-testid": testId, "data-status": status?.kind, "data-tone": status?.tone };
  const anchor = status?.fix?.to === "setup" ? status.fix.anchor : undefined;
  return (
    <span className={cx(SEGMENT, selected ? SELECTED : UNSELECTED)}>
      {href ? (
        <Link href={href} aria-current={selected ? "page" : undefined} title={hint} className={nameClass} {...marks}>
          {label}
        </Link>
      ) : (
        <span aria-disabled="true" title={hint} className={nameClass} {...marks}>
          {label}
        </span>
      )}
      {word ??
        (status?.word &&
          (fixHref && status.fix ? (
            <Link
              href={fixHref}
              onClick={anchor ? () => focusAnchor(anchor) : undefined}
              aria-label={`${name} ${status.word}: ${FIX_WORDS[status.fix.to]}`}
              title={`${name}: ${status.detail}. Go to the fix.`}
              className={cx(
                FOCUS,
                "-ml-1 max-w-36 min-w-0 truncate pr-2.25 font-medium group-data-[tight=true]/bar:max-w-20 hover:underline",
                selected ? "text-panel" : WORDS[tone],
              )}
              data-testid={`${testId}-status`}
              data-tone={tone}
            >
              · {status.word}
            </Link>
          ) : (
            <span
              title={`${name}: ${status.detail}`}
              className={cx(
                "-ml-1 max-w-36 min-w-0 truncate pr-2.25 font-medium group-data-[tight=true]/bar:max-w-20",
                selected ? "text-panel" : WORDS[tone],
              )}
              data-testid={`${testId}-status`}
              data-tone={tone}
            >
              · {status.word}
            </span>
          )))}
    </span>
  );
}
