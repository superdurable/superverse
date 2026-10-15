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
import { startDurationWords } from "@/lib/project/setup/environment-status";
import { cannotCheckText } from "@/lib/shared/problem/problem";
import { requirementGapName } from "@/lib/shared/problem/requirement-gaps";
import { ACTION_CELLS, ACTION_COLUMNS } from "@/components/flow/scope/SetupChecklistSurface";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { Spinner } from "@/components/ui/Icons";

const plural = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`;

/** A link, in red, that opens and focuses one item Setup misses. */
function ItemLink({
  anchor,
  hrefOf,
  children,
}: {
  anchor: string;
  hrefOf: (anchor: string) => string;
  children: ReactNode;
}) {
  return (
    <Link
      href={hrefOf(anchor)}
      onClick={() => focusAnchor(anchor)}
      className="text-fail underline decoration-fail-rule underline-offset-3"
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
 * The Setup bar above the table, on its columns. Over the Preview column, Apply to Preview, which applies at once
 * what Setup applies on its own once edits settle, with Preview's state under it: what it lacks, as links to
 * each item, the saved changes it is being updated with, or the version it runs with these settings. Over the
 * Next publish column, Publish, which opens the Publish popover whenever something waits, with the next
 * publish's state under it: what to set up before publishing, what waits, or the version ready to publish.
 */
export function SetupBar({
  configuration,
  sourceReady,
  preview,
  updating,
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
}: {
  /** Preview's configuration of its prepared source, null while it is read. */
  configuration: ProjectConfigurationSnapshot__Output | null;
  sourceReady: boolean;
  preview: PreviewSnapshot__Output | null;
  /** Setup is about to apply its saved changes to the running Preview on its own. */
  updating: boolean;
  canApply: boolean;
  /** An apply, or a start that takes the saved changes, is being sent. */
  applying: boolean;
  /** Applies the saved settings now: restarts a running Preview with them, or starts a stopped one. */
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
  /** Opens the Publish popover on the newest version, or on Live's waiting settings; null with nothing to publish. */
  onPublish: (() => void) | null;
  /** This page with an anchor. */
  hrefOf: (anchor: string) => string;
  /** The version Preview runs, "V12"; null before it runs one. */
  version: string | null;
  /** Each connector's name, by connector ID, for the Next publish line's words. */
  connectors: Record<string, string>;
}) {
  const ready = configuration?.status === "CONFIGURATION_STATUS_READY";
  const changes = configuration?.unappliedChangeCount ?? 0;
  const running = preview?.status === "PREVIEW_STATUS_READY";
  const source = preview?.preparedSource;
  let previewLine: ReactNode;
  if (!sourceReady)
    previewLine = (
      <span className="text-ink-faint">
        {source?.status === "PREVIEW_SOURCE_STATUS_PREPARING"
          ? "Building the latest push… Setup opens when it is ready."
          : source?.status === "PREVIEW_SOURCE_STATUS_FAILED"
            ? "The latest push did not build; Build processes below says why."
            : "The latest push is not built yet; Build processes below builds it."}
      </span>
    );
  else if (!configuration)
    previewLine = <span className="text-ink-faint">Reading the Preview&apos;s configuration…</span>;
  else if (applying || preview?.status === "PREVIEW_STATUS_STARTING")
    previewLine = <span className="text-ink-dim">Updating Preview…</span>;
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
        {updating
          ? changes > 0
            ? `Updating Preview with ${plural(changes, "change", "changes")}…`
            : "Updating Preview…"
          : changes > 0
            ? running
              ? `${plural(changes, "change", "changes")} not on Preview yet`
              : `${plural(changes, "change", "changes")} not on Preview yet; it takes them when it starts`
            : running
              ? `Running ${version ? `${version} ` : ""}with these settings`
              : "Preview is not running; it starts with these settings"}
      </span>
    );
  const previewGaps = new Set((configuration?.requirementGaps ?? []).map(gapItemKey));
  const ownGaps = (next?.requirementGaps ?? []).filter((gap) => !previewGaps.has(gapItemKey(gap)));
  let nextLine: ReactNode;
  if (!sourceReady) nextLine = <span className="text-ink-faint">Shows once the latest push is built.</span>;
  else if (!next)
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
  const state = "flex flex-wrap items-baseline gap-x-1.5 gap-y-1 text-[12.5px] leading-snug";
  return (
    <section aria-label="Setup status" className={ACTION_COLUMNS} data-testid="setup-bar">
      <span className={ACTION_CELLS.saving}>{saving}</span>
      <Button
        size="sm"
        className={ACTION_CELLS.apply}
        disabled={!sourceReady || !canApply || !ready || (running && changes === 0)}
        title={
          !ready
            ? "Complete what is missing first"
            : running && changes === 0
              ? "Preview runs these settings"
              : "Setup applies your changes on its own once you stop editing; this applies them now"
        }
        onClick={onApply}
      >
        {applying ? (
          <Spinner className="size-3.5" />
        ) : (
          <span aria-hidden="true" className="size-2 rounded-full border-[1.5px] border-current" />
        )}
        {applying ? "Applying…" : "Apply to Preview"}
      </Button>
      <Button
        size="sm"
        className={ACTION_CELLS.publish}
        disabled={!onPublish}
        title={onPublish ? undefined : "Nothing waits for a publish"}
        onClick={() => onPublish?.()}
        data-testid="setup-publish"
      >
        <Glyph name="upload" className="size-3.5" />
        Publish
      </Button>
      <div className={cx(ACTION_CELLS.preview, state)} aria-label="Preview" data-testid="apply-header">
        {previewLine}
        {sourceReady && ready && (changes > 0 || !running) && (
          <span className="text-[12px] text-ink-faint">
            · {running ? "restarts" : "starts"} in {startDurationWords(preview)}
          </span>
        )}
      </div>
      <div className={cx(ACTION_CELLS.next, state)} aria-label="Next publish" data-testid="next-publish-line">
        {nextLine}
      </div>
    </section>
  );
}
