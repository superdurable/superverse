// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { nextPublishItemAnchor } from "@/lib/api/flow/draft/configuration-draft";
import { focusAnchor } from "@/lib/browser/hash-focus";
import type { ConfigurationItem__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItem";
import type { ConfigurationRequirementGap__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationRequirementGap";
import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import { itemKeyOf, itemName, nextPublishGapAnchor } from "@/lib/project/setup/configuration-items";
import { cannotCheckText } from "@/lib/shared/problem/problem";
import { requirementGapName } from "@/lib/shared/problem/requirement-gaps";
import { cx } from "@/components/ui/cx";

const SECTION = "px-1 pt-2.5 pb-1 text-[11px] tracking-[0.07em] text-ink-faint uppercase";

/** One line of a list: a dot in its tone, the words, and the link to its Setup row. */
function Line({
  tone,
  words,
  link,
  href,
  anchor,
  onNavigate,
}: {
  tone: "fail" | "busy";
  words: string;
  link: string;
  href: string;
  anchor: string;
  onNavigate: () => void;
}) {
  return (
    <li className="flex items-start gap-2 px-1 py-1.25 text-[12.5px]">
      <span
        aria-hidden="true"
        className={cx("mt-1.5 size-2 shrink-0 rounded-full", tone === "fail" ? "bg-fail" : "bg-busy")}
      />
      <span className="min-w-0 flex-1 leading-snug text-ink">{words}</span>
      <Link
        href={href}
        className="shrink-0 whitespace-nowrap text-accent hover:underline"
        onClick={() => {
          onNavigate();
          focusAnchor(anchor);
        }}
      >
        {link}
      </Link>
    </li>
  );
}

/** A gap in words; a secret or an address-bound connection says why it cannot follow Preview. */
function gapWords(gap: ConfigurationRequirementGap__Output): string {
  if (gap.followRefusal === "CONFIGURATION_FOLLOW_REFUSAL_SECRET")
    return `${gap.environmentName || gap.connectionName} needs its own value: a secret never copies from Preview`;
  if (gap.followRefusal === "CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND")
    return `${gap.connectionName} needs its own connection: it answers at one address`;
  return requirementGapName(gap);
}

/** What deserves a look on an item and does not block, in words; null when nothing does. */
function checkWords(item: ConfigurationItem__Output): string | null {
  if (!item.key || item.state === "STATE_NO_LONGER_USED") return null;
  const name = itemName(item.key);
  if (item.warnings.includes("CONFIGURATION_ITEM_WARNING_CONNECTION_DELETED"))
    return `${name}: its connection was deleted or is no longer active`;
  if (item.warnings.includes("CONFIGURATION_ITEM_WARNING_CHANNEL_SHARED"))
    return `${name}: ${item.warningProjectName || "another Project"} listens there too`;
  if (item.key.kind === "KIND_TRIGGER_BINDING" && item.state === "STATE_FOLLOWS_PREVIEW" && item.previewValueJson)
    return `${name} listens where Preview listens`;
  return null;
}

/** The anchor of an item's Next publish control in Setup, which unfolds its row and focuses the control. */
function itemAnchor(item: ConfigurationItem__Output): string {
  return nextPublishItemAnchor(itemKeyOf(item.key!));
}

/**
 * What the popover lists before publishing a version: what blocks it ("Set up for Live first"), each gap
 * with "Set up ›" to its Setup row with the Next publish control focused, and what deserves a look without
 * blocking ("Check"), such as a trigger listening where Preview listens, with "Pick ›".
 */
export function PublishChecks({
  configuration,
  setupHref,
  onNavigate,
}: {
  configuration: ProjectConfigurationSnapshot__Output;
  /** Setup on Live's next publish, focused on the element `anchor` names. */
  setupHref: (anchor: string) => string;
  onNavigate: () => void;
}) {
  const ready = configuration.status === "CONFIGURATION_STATUS_READY";
  const gaps = ready ? [] : configuration.requirementGaps;
  const checks = configuration.items.flatMap((item) => {
    const words = checkWords(item);
    return words ? [{ item, words }] : [];
  });
  return (
    <>
      {!ready && (
        <section aria-label="Set up for Live first" data-testid="publish-blockers">
          <h3 className={SECTION}>Set up for Live first · {gaps.length || 1}</h3>
          <ul>
            {gaps.map((gap) => {
              const anchor = nextPublishGapAnchor(gap);
              return (
                <Line
                  key={`${anchor}:${requirementGapName(gap)}`}
                  tone="fail"
                  words={gapWords(gap)}
                  link="Set up ›"
                  href={setupHref(anchor)}
                  anchor={anchor}
                  onNavigate={onNavigate}
                />
              );
            })}
            {gaps.length === 0 && (
              <li className="px-1 py-1.25 text-[12.5px] text-busy">
                {cannotCheckText(configuration.validationErrors)}
              </li>
            )}
          </ul>
        </section>
      )}
      {checks.length > 0 && (
        <section aria-label="Check" data-testid="publish-checks">
          <h3 className={SECTION}>Check</h3>
          <ul>
            {checks.map(({ item, words }) => (
              <Line
                key={itemAnchor(item)}
                tone="busy"
                words={words}
                link="Pick ›"
                href={setupHref(itemAnchor(item))}
                anchor={itemAnchor(item)}
                onNavigate={onNavigate}
              />
            ))}
          </ul>
        </section>
      )}
    </>
  );
}
