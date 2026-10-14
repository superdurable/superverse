// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import type { ConfigurationRequirementGap__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationRequirementGap";
import type { PreviewSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewSnapshot";
import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import { focusAnchor } from "@/lib/browser/hash-focus";
import { gapAnchor, gapItemKey, nextPublishGapAnchor } from "@/lib/project/setup/configuration-items";
import { previewStatusOf, startDurationWords } from "@/lib/project/setup/environment-status";
import { cannotCheckText } from "@/lib/shared/problem/problem";
import { requirementGapName } from "@/lib/shared/problem/requirement-gaps";
import { statusDotClass } from "@/components/frame/status/EnvironmentStatusTab";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { Spinner } from "@/components/ui/Icons";

const LINE = cx("flex min-h-11.5 flex-wrap items-center gap-x-2.5 gap-y-1.5 px-3.5 py-2.25 text-[13px]");
const plural = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`;

/** A link that opens and focuses one item: red where something is missing, amber where it deserves a look. */
function ItemLink({
  anchor,
  hrefOf,
  amber = false,
  children,
}: {
  anchor: string;
  hrefOf: (anchor: string) => string;
  amber?: boolean;
  children: ReactNode;
}) {
  return (
    <Link
      href={hrefOf(anchor)}
      onClick={() => focusAnchor(anchor)}
      className={cx(
        "underline underline-offset-3",
        amber ? "text-busy decoration-busy-rule" : "text-fail decoration-fail-rule",
      )}
    >
      {children}
    </Link>
  );
}

/** Each gap as a link that opens and focuses its item, in `wordsOf`'s words. */
function GapLinks({
  label,
  gaps,
  anchorOf,
  hrefOf,
  wordsOf = requirementGapName,
}: {
  label: string;
  gaps: readonly ConfigurationRequirementGap__Output[];
  anchorOf: (gap: ConfigurationRequirementGap__Output) => string;
  hrefOf: (anchor: string) => string;
  wordsOf?: (gap: ConfigurationRequirementGap__Output) => string;
}) {
  return (
    <ul aria-label={label} className="flex flex-wrap items-center gap-x-2 gap-y-1">
      {gaps.map((gap) => (
        <li key={`${gapItemKey(gap)}:${requirementGapName(gap)}`}>
          <ItemLink anchor={anchorOf(gap)} hrefOf={hrefOf}>
            {wordsOf(gap)}
          </ItemLink>
        </li>
      ))}
    </ul>
  );
}

/**
 * What the next publish lacks, in plain words with the connector's name: "Stripe needs its own
 * connection", "UNSUBSCRIBE_SIGNING_KEY needs its own value", "Slack: channel".
 */
function nextPublishGapWords(gap: ConfigurationRequirementGap__Output, connectors: Record<string, string>): string {
  const name = connectors[gap.connectorId] || gap.connectorId;
  const setting = (fallback: string) => (gap.unitLabel || fallback).toLowerCase();
  switch (gap.kind) {
    case "KIND_CONNECTION_REQUIRED":
      return gap.followRefusal === "CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND"
        ? `${name} needs its own connection`
        : `${name}: pick a connection`;
    case "KIND_AUTHORIZATION_REQUIRED":
      return `${name}: sign in again`;
    case "KIND_CONNECTION_INCOMPLETE":
      return `${name}: finish its connection`;
    case "KIND_TRIGGER_BINDING_REQUIRED":
    case "KIND_TRIGGER_BINDING_INVALID":
      return `${name}: ${setting("trigger setting")}`;
    case "KIND_OPERATION_CONFIGURATION_REQUIRED":
      return `${gap.stepName || name}: ${setting("setting")}`;
    case "KIND_ENVIRONMENT_REQUIRED":
      return gap.followRefusal === "CONFIGURATION_FOLLOW_REFUSAL_SECRET"
        ? `${gap.environmentName} needs its own value`
        : gap.environmentName;
    case "KIND_UNSPECIFIED":
      return requirementGapName(gap);
  }
}

/**
 * Setup's two lines above the table. Preview: what it lacks, as links to each item, or the saved changes it
 * does not run yet with Apply to Preview, which restarts it with them, or the version it runs with these
 * settings. Next publish, Live's settings: what to set up before publishing, what waits for a publish,
 * or the version ready to publish, with Publish….
 */
export function SetupBar({
  configuration,
  sourceReady,
  preview,
  canApply,
  applying,
  onApply,
  saving,
  next,
  nextError,
  waiting,
  ahead,
  onPublish,
  hrefOf,
  version,
  connectors,
  warnings,
}: {
  /** Preview's configuration of its prepared source, null while it is read. */
  configuration: ProjectConfigurationSnapshot__Output | null;
  sourceReady: boolean;
  preview: PreviewSnapshot__Output | null;
  canApply: boolean;
  applying: boolean;
  onApply: () => void;
  /** "Saving…" or "Saved" while this tab's Setup writes settle, of either column. */
  saving: string | null;
  /** The next publish, checked against Preview's source; null while it is read. */
  next: ProjectConfigurationSnapshot__Output | null;
  nextError: string;
  /** How many settings differ from what current Live runs. */
  waiting: number;
  /** The newest version ahead of Live, "V13"; null when Live runs the newest. */
  ahead: string | null;
  /** Opens the Publish popover on the newest version; null where nothing opens it. */
  onPublish: (() => void) | null;
  /** This page with an anchor. */
  hrefOf: (anchor: string) => string;
  /** The version Preview runs, "V12"; null before it runs one. */
  version: string | null;
  /** Each connector's name, by connector ID, for the Next publish line's words. */
  connectors: Record<string, string>;
  /** What the next publish would do that deserves a look, each a link to its control. */
  warnings: readonly { anchor: string; words: string }[];
}) {
  const status = previewStatusOf(preview, configuration);
  const ready = configuration?.status === "CONFIGURATION_STATUS_READY";
  const changes = configuration?.unappliedChangeCount ?? 0;
  const running = preview?.status === "PREVIEW_STATUS_READY";
  let previewLine: ReactNode;
  if (!sourceReady)
    previewLine = <span className="text-ink-faint">The latest push is not prepared yet; Setup opens once it is.</span>;
  else if (!configuration)
    previewLine = <span className="text-ink-faint">Reading the Preview&apos;s configuration…</span>;
  else if (applying || preview?.status === "PREVIEW_STATUS_STARTING")
    previewLine = <span className="text-ink-dim">Restarting with your changes…</span>;
  else if (!ready && configuration.requirementGaps.length > 0)
    previewLine = (
      <>
        <span>{configuration.requirementGaps.length} missing:</span>
        <GapLinks
          label="Missing configuration"
          gaps={configuration.requirementGaps}
          anchorOf={gapAnchor}
          hrefOf={hrefOf}
        />
      </>
    );
  else if (!ready) previewLine = <span className="text-busy">{cannotCheckText(configuration.validationErrors)}</span>;
  else
    previewLine = (
      <span className={changes > 0 ? "text-ink" : "text-ink-faint"} data-testid="unapplied-count">
        {changes > 0
          ? `${plural(changes, "change", "changes")} not on Preview yet`
          : running
            ? `Running ${version ? `${version} ` : ""}with these settings`
            : "No changes to apply"}
      </span>
    );
  const previewGaps = new Set((configuration?.requirementGaps ?? []).map(gapItemKey));
  const ownGaps = (next?.requirementGaps ?? []).filter((gap) => !previewGaps.has(gapItemKey(gap)));
  let nextLine: ReactNode;
  if (!next)
    nextLine = (
      <span className={nextError ? "text-fail" : "text-ink-faint"}>{nextError || "Reading Live's next publish…"}</span>
    );
  else if (previewGaps.size > 0)
    nextLine =
      ownGaps.length > 0 ? (
        <>
          <span>Follows Preview once Preview is set up, plus {ownGaps.length} of its own:</span>
          <GapLinks
            label="Set up for Live"
            gaps={ownGaps}
            anchorOf={nextPublishGapAnchor}
            hrefOf={hrefOf}
            wordsOf={(gap) => nextPublishGapWords(gap, connectors)}
          />
        </>
      ) : (
        <span className="text-ink-faint">Follows Preview once Preview is set up</span>
      );
  else if (next.requirementGaps.length > 0)
    nextLine = (
      <>
        <span>{next.requirementGaps.length} to set up before publishing:</span>
        <GapLinks
          label="Set up for Live"
          gaps={next.requirementGaps}
          anchorOf={nextPublishGapAnchor}
          hrefOf={hrefOf}
          wordsOf={(gap) => nextPublishGapWords(gap, connectors)}
        />
      </>
    );
  else if (next.status !== "CONFIGURATION_STATUS_READY")
    nextLine = <span className="text-busy">{cannotCheckText(next.validationErrors)}</span>;
  else
    nextLine = (
      <span data-testid="next-publish-state">
        {ahead
          ? `Ready to publish ${ahead} with these settings`
          : waiting > 0
            ? `${plural(waiting, "change", "changes")} waiting for a publish`
            : "Live runs these settings"}
      </span>
    );
  return (
    <section
      aria-label="Setup status"
      className="max-w-240 rounded-[10px] border border-line-soft bg-panel"
      data-testid="setup-bar"
    >
      <div className={LINE} data-testid="apply-header">
        <span className={cx("size-2 shrink-0 rounded-full border", statusDotClass("preview", status.tone))} />
        <b className="font-semibold">Preview</b>
        {previewLine}
        {saving && <span className="text-[12px] text-ink-faint">{saving}</span>}
        {sourceReady && configuration && (
          <span className="ml-auto flex items-center gap-2">
            {ready && (changes > 0 || !running) && (
              <span className="text-[12px] text-ink-faint">
                {running ? "restarts" : "starts"} in {startDurationWords(preview)}
              </span>
            )}
            <Button
              variant="strong"
              size="sm"
              disabled={!canApply || !ready}
              title={!ready ? "Complete what is missing first" : undefined}
              onClick={onApply}
            >
              {applying && <Spinner className="size-3.5" />}
              {applying ? "Applying…" : "Apply to Preview"}
            </Button>
          </span>
        )}
      </div>
      <div className={cx(LINE, "border-t border-line-faint")} data-testid="next-publish-line">
        <Glyph name="upload" className="size-3.5 text-ink-dim" />
        <b className="font-semibold">Next publish</b>
        <span
          className="text-ink-faint"
          title="The same settings as the Live tab: a change in either place is the same change"
        >
          · Live&apos;s settings
        </span>
        {nextLine}
        {next && warnings.length > 0 && (
          <ul aria-label="Check before publishing" className="flex flex-wrap items-center gap-x-2 gap-y-1">
            {warnings.map((warning) => (
              <li key={warning.anchor}>
                <ItemLink anchor={warning.anchor} hrefOf={hrefOf} amber>
                  {warning.words}
                </ItemLink>
              </li>
            ))}
          </ul>
        )}
        {onPublish && ahead && (
          <Button size="sm" className="ml-auto" onClick={onPublish} data-testid="setup-publish">
            <Glyph name="upload" className="size-3.5" />
            Publish…
          </Button>
        )}
      </div>
    </section>
  );
}
