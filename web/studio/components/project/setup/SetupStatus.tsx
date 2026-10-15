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
 * Preview's state, under the Preview column's name: what it lacks, as links to each item; the saved changes
 * Setup is updating it with, which it applies on its own once edits settle; or the version it runs with these
 * settings.
 */
export function PreviewStatus({
  configuration,
  preview,
  updating,
  pushUpdating,
  applying,
  hrefOf,
  version,
}: {
  /** Preview's configuration of its prepared source, null while it is read. */
  configuration: ProjectConfigurationSnapshot__Output | null;
  preview: PreviewSnapshot__Output | null;
  /** Setup is about to apply its saved changes to the running Preview on its own. */
  updating: boolean;
  /** A push's automatic update runs; Setup's changes follow it. */
  pushUpdating: boolean;
  /** An apply, or a start that takes the saved changes, is being sent. */
  applying: boolean;
  /** This page with an anchor. */
  hrefOf: (anchor: string) => string;
  /** The version Preview runs, "V12"; null before it runs one. */
  version: string | null;
}) {
  const ready = configuration?.status === "CONFIGURATION_STATUS_READY";
  const changes = configuration?.unappliedChangeCount ?? 0;
  const running = preview?.status === "PREVIEW_STATUS_READY";
  let line: ReactNode;
  if (!configuration) line = <span className="text-ink-faint">Reading the Preview&apos;s configuration…</span>;
  else if (pushUpdating)
    line = <span className="text-ink-dim">Updating to the latest push…{changes > 0 && " Your changes follow."}</span>;
  else if (applying || preview?.status === "PREVIEW_STATUS_STARTING")
    line = <span className="text-ink-dim">Updating Preview…</span>;
  else if (!ready && configuration.requirementGaps.length > 0)
    line = (
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
  else if (!ready) line = <span className="text-busy">{cannotCheckText(configuration.validationErrors)}</span>;
  else
    line = (
      <span className={changes > 0 ? "text-ink" : "text-ink-faint"} data-testid="unapplied-count">
        {updating
          ? changes > 0
            ? `Updating Preview with ${plural(changes, "change", "changes")}…`
            : "Updating Preview…"
          : changes > 0
            ? running
              ? `${plural(changes, "change", "changes")} not on Preview yet`
              : `${plural(changes, "change", "changes")} saved; Preview takes them when it starts`
            : running
              ? `Running ${version ? `${version} ` : ""}with these settings`
              : "Not running; starts with these settings"}
      </span>
    );
  return (
    <>
      {line}
      {configuration &&
        ready &&
        !pushUpdating &&
        (updating || applying || preview?.status === "PREVIEW_STATUS_STARTING") && (
          <span className="text-[12px] text-ink-faint">· takes {startDurationWords(preview)}</span>
        )}
    </>
  );
}

/**
 * The next publish's state, under the Next publish column's name or Live's: what to set up before publishing,
 * as links to each item; what waits for a publish; or the version ready to publish. The bar's Publish publishes.
 */
export function NextPublishStatus({
  previewConfiguration,
  next,
  nextError,
  waiting,
  ahead,
  hrefOf,
  connectors,
}: {
  /** Preview's configuration, whose gaps the next publish follows; null on the Live tab. */
  previewConfiguration: ProjectConfigurationSnapshot__Output | null;
  /** The next publish, checked against its version; null while it is read. */
  next: ProjectConfigurationSnapshot__Output | null;
  nextError: string;
  /** How many settings differ from what current Live runs. */
  waiting: number;
  /** The newest version ahead of Live, "V13"; null when Live runs the newest, and on the Live tab. */
  ahead: string | null;
  /** This page with an anchor. */
  hrefOf: (anchor: string) => string;
  /** Each connector's name, by connector ID, for the gaps' words. */
  connectors: Record<string, string>;
}) {
  const previewGaps = new Set((previewConfiguration?.requirementGaps ?? []).map(gapItemKey));
  const ownGaps = (next?.requirementGaps ?? []).filter((gap) => !previewGaps.has(gapItemKey(gap)));
  if (!next)
    return (
      <span className={nextError ? "text-fail" : "text-ink-faint"}>{nextError || "Reading Live's next publish…"}</span>
    );
  if (previewGaps.size > 0)
    return ownGaps.length > 0 ? (
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
  if (next.requirementGaps.length > 0)
    return (
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
  if (next.status !== "CONFIGURATION_STATUS_READY")
    return <span className="text-busy">{cannotCheckText(next.validationErrors)}</span>;
  return (
    <span className={ahead || waiting > 0 ? "text-ink" : "text-ink-faint"} data-testid="next-publish-state">
      {ahead
        ? `Ready to publish ${ahead} with these settings`
        : waiting > 0
          ? `${plural(waiting, "change", "changes")} waiting for a publish`
          : "Live runs these settings"}
    </span>
  );
}

/** Why Setup's table is not shown yet: the latest push is building, failed to build, or is not built. */
export function UnbuiltStatus({ preview }: { preview: PreviewSnapshot__Output | null }) {
  const source = preview?.preparedSource;
  return (
    <p className="text-[13px] text-ink-faint" role="status" data-testid="preview-status">
      {source?.status === "PREVIEW_SOURCE_STATUS_PREPARING"
        ? "Building the latest push… Setup opens when it is ready."
        : source?.status === "PREVIEW_SOURCE_STATUS_FAILED"
          ? "The latest push did not build; Build processes below says why."
          : "The latest push is not built yet; Build processes below builds it."}
    </p>
  );
}
