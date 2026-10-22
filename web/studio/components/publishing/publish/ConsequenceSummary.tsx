// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { getDeployment } from "@/lib/api/deployment";
import { getRelease } from "@/lib/api/release";
import type { ConfigurationItem__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationItem";
import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import type { ProjectLive__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectLive";
import { useProcessDefinitions } from "@/lib/project/ProcessDefinitionsProvider";
import { timingSpan } from "@/lib/project/publishing/publish-stages";
import { itemName, itemValueText, liveDifferenceCount } from "@/lib/project/setup/configuration-items";
import { formatElapsedDuration, processName } from "@/lib/shared/format";
import { flowApiRoutes } from "@/lib/shared/studio-routes";
import { versionNumber } from "@/lib/shared/version/version";
import { LiveIdentitiesSurface } from "@/components/flow/scope/LiveIdentitiesSurface";

/** How long the previous publish's build and its deployment took, read once; null before or without one. */
function usePreviousDurations(projectId: string, live: ProjectLive__Output | null) {
  const [read, setRead] = useState<{ key: string; build: number | null; deploy: number | null } | null>(null);
  const key = live ? `${live.releaseId}/${live.buildRevisionId}/${live.deploymentId}` : "";
  useEffect(() => {
    if (!live) return;
    const controller = new AbortController();
    Promise.all([
      getRelease({ projectId, releaseId: live.releaseId }, controller.signal),
      getDeployment({ projectId, deploymentId: live.deploymentId }, controller.signal),
    ]).then(
      ([release, deployment]) => {
        const build = release.release?.buildRevisions.find((item) => item.buildRevisionId === live.buildRevisionId);
        if (!controller.signal.aborted)
          setRead({
            key,
            build: timingSpan(build?.phaseTimings ?? []),
            deploy: timingSpan(deployment?.phaseTimings ?? []),
          });
      },
      // Without the previous durations the stages are named without them.
      () => !controller.signal.aborted && setRead({ key, build: null, deploy: null }),
    );
    return () => controller.abort();
  }, [key, live, projectId]);
  return read?.key === key ? read : { build: null, deploy: null };
}

/** Names in a sentence: "A", "A and B", "A, B and C". */
function joined(names: readonly string[]): string {
  return names.length < 2 ? names.join("") : `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

/** Whether an item is a secret, whose value is never shown. */
const isSecret = (item: ConfigurationItem__Output) => item.followRefusal === "CONFIGURATION_FOLLOW_REFUSAL_SECRET";

/** An item's name in a sentence. */
const named = (item: ConfigurationItem__Output) => (item.key ? itemName(item.key) : "a setting");

/** A waiting setting in words, with Live's value beside it unless it is a secret. */
function waitingWords(item: ConfigurationItem__Output): string {
  const now = item.liveValueJson ? `Live now: ${itemValueText(item.liveValueJson)}` : "a secret, not shown";
  return `${named(item)} changes (${now}).`;
}

/**
 * What a publish does, before it does it: one line first, such as "Replaces V12 · 2 setting changes · about
 * 3 min" or "V12 again · 1 setting change", then Details, folded: the version it replaces (or a first
 * publish), the processes that change or start, each waiting setting with Live's value, an address-bound
 * connection that sends events to Live's own address, the items no longer used, the connections Live runs
 * with, and the time it took last time. A settings-only publish keeps Live's code and lists only the settings.
 */
export function ConsequenceSummary({
  projectId,
  commit,
  configuration,
  live,
  settingsOnly,
}: {
  projectId: string;
  commit: string;
  configuration: ProjectConfigurationSnapshot__Output;
  live: ProjectLive__Output | null;
  settingsOnly: boolean;
}) {
  const durations = usePreviousDurations(projectId, live);
  const definitions = useProcessDefinitions();
  const [identities, setIdentities] = useState(false);
  const used = configuration.items.filter((item) => item.state !== "STATE_NO_LONGER_USED");
  const onLive = (item: ConfigurationItem__Output) => Boolean(item.liveValueJson) || isSecret(item);
  const waiting = used.filter((item) => item.differsFromLive && onLive(item));
  const added = live ? used.filter((item) => item.differsFromLive && !onLive(item)) : [];
  const ownAddress = used.filter((item) => item.followRefusal === "CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND");
  const gone = configuration.items.filter((item) => item.state === "STATE_NO_LONGER_USED");
  const changed =
    live && !settingsOnly && definitions.previewCommit === commit
      ? definitions.processes.filter((process) => definitions.changedSinceLive.has(process.flowType))
      : [];
  const build = durations.build ? `Build about ${formatElapsedDuration(durations.build)}` : "Build";
  const deploy = durations.deploy
    ? `deploy and a health check, about ${formatElapsedDuration(durations.deploy)}`
    : "deploy and a health check";
  const count = liveDifferenceCount(configuration);
  const total = durations.build && durations.deploy ? durations.build + durations.deploy : null;
  const headline = [
    settingsOnly
      ? `${versionNumber(live?.version)} again`
      : live
        ? `Replaces ${versionNumber(live.version)}`
        : "First publish to Live",
    count > 0 ? `${count} setting change${count === 1 ? "" : "s"}` : settingsOnly ? "same settings" : "",
    total ? `about ${formatElapsedDuration(total)}` : "",
  ]
    .filter(Boolean)
    .join(" · ");
  const lines = [
    settingsOnly
      ? `Live keeps running ${versionNumber(live?.version)}'s code; it rebuilds with ${waiting.length + added.length + gone.length > 0 ? "the new" : "the same"} settings.`
      : live
        ? `Replaces ${versionNumber(live.version)}. Runs in flight move to this version when it goes live; new runs start on it.`
        : "Starts Live at its address for the first time.",
    changed.length > 0 &&
      `${joined(changed.map((process) => processName(process.flowType)))} change${changed.length === 1 ? "s" : ""} or start on Live.`,
    added.length > 0 && `New on Live: ${joined(added.map(named))}.`,
    ...waiting.map(waitingWords),
    ...ownAddress.map((item) => `${named(item)} sends events to Live's own address.`),
    gone.length > 0 && `No longer used: ${joined(gone.map(named))}.`,
    `${build}, then ${deploy}.`,
  ].filter((line): line is string => Boolean(line));
  return (
    <>
      <p className="px-1 pb-1 text-[13px] text-ink" data-testid="publish-headline">
        {headline}
      </p>
      <details className="group px-1 pb-1 text-[12.5px]" data-testid="publish-consequences">
        <summary className="cursor-pointer text-ink-faint">Details</summary>
        <ul className="mt-1 grid list-disc gap-1 pl-4 text-ink-dim marker:text-ink-faint">
          {lines.map((line, index) => (
            <li key={line} data-testid={index === 0 ? "publish-replaces" : undefined}>
              {line}
            </li>
          ))}
          <li>
            <button
              type="button"
              className="text-accent hover:underline"
              aria-expanded={identities}
              onClick={() => setIdentities((value) => !value)}
            >
              {identities ? "Hide the connections Live uses" : "The connections Live uses"}
            </button>
          </li>
        </ul>
        {identities && (
          <div className="mt-1.5 rounded-lg bg-well px-2.5 py-2">
            <LiveIdentitiesSurface
              basePath={flowApiRoutes.liveNextPublish(projectId, configuration.releaseId || null)}
              version={configuration.revision}
            />
          </div>
        )}
      </details>
    </>
  );
}
