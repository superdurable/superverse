// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState, type ReactNode } from "react";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import type { RecentTriggerEvent__Output } from "@/lib/generated/protobuf/superverse/v2/RecentTriggerEvent";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { REMEDIES } from "@/lib/shared/problem/problem";
import { timestampToISO } from "@/lib/shared/protobuf";
import { useRecentEvents } from "./RecentEventsProvider";
import {
  countedKinds,
  fateDelivered,
  fateRemedy,
  fateWhy,
  fateWords,
  type EventEnvironment,
} from "./trigger-event-fates";

/**
 * One line above the runs while recent events did not arrive: how many of the newest problem's kind,
 * why, and the one action that fixes it. Starting the environment comes from the page as `start`,
 * where it can start; the agent fixes what the application refused, and an operator gets the details
 * of what the platform dropped. The relay keeps only recent events, so the line says "recently".
 */
export function RecentEventsProblem({ start }: { start?: ReactNode }) {
  const read = useRecentEvents();
  const { askAgent } = useFlowHost();
  if (!read) return null;
  // Newest first, as the relay lists them.
  const problems = read.events.filter((event) => !fateDelivered(event.fate));
  const newest = problems[0];
  if (!newest) return null;
  const environment = read.environment;
  const same = problems.filter((event) => event.fate === newest.fate);
  const others = problems.length - same.length;
  const line = `${countedKinds(same.map((event) => event.eventType))} didn't reach ${environment} recently: ${fateWhy(newest.fate, environment, same.length)}.`;
  const remedy = fateRemedy(newest.fate);
  const details = detailsText(line, same, environment);
  const action =
    remedy === "start" ? (
      start
    ) : remedy === "agent" && askAgent ? (
      <Button
        size="sm"
        onClick={() =>
          askAgent(
            `${environment} ▸ Recent events`,
            `${details}\nFind out why and fix the application${environment === "Live" ? " on Preview" : ""}.`,
          )
        }
      >
        <Glyph name="spark" className="size-3.5" />
        Ask the agent
      </Button>
    ) : (
      <CopyDetails text={details} />
    );
  // Spans only: a stopped Preview's notice holds this line inside its paragraph.
  return (
    <span
      role="status"
      className="mb-2.5 block shrink-0 rounded-md bg-busy-subtle px-2.5 py-2 text-caption text-busy"
      data-testid="recent-events-problem"
      data-remedy={remedy ?? undefined}
    >
      <span className="block">
        {line}
        {others > 0 && ` ${others} more didn't arrive either.`}
      </span>
      {action && <span className="mt-1.5 flex [&_button]:mt-0">{action}</span>}
    </span>
  );
}

/** Copies the details for the operator, as a problem's operator remedy does. */
function CopyDetails({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      size="sm"
      onClick={() =>
        void navigator.clipboard.writeText(text).then(
          () => setCopied(true),
          () => setCopied(false),
        )
      }
    >
      {copied ? "Copied" : REMEDIES.FAILURE_REMEDY_CONTACT_OPERATOR.label}
    </Button>
  );
}

/** What the agent or an operator needs: the line, then each event's own fields, never its text. */
function detailsText(line: string, events: readonly RecentTriggerEvent__Output[], environment: EventEnvironment) {
  const rows = events.map((event) =>
    [
      timestampToISO(event.receivedAt) ?? "",
      event.eventSubtype ? `${event.eventType} · ${event.eventSubtype}` : event.eventType,
      fateWords(event.fate, environment),
      event.httpStatus ? `HTTP ${event.httpStatus}` : "",
      event.connectionName && `connection ${event.connectionName}`,
      event.teamId && `team ${event.teamId}`,
      event.channelId && `channel ${event.channelId}`,
      event.eventId && `event ${event.eventId}`,
    ]
      .filter(Boolean)
      .join(", "),
  );
  return [line, ...rows.map((row) => `- ${row}`)].join("\n");
}
