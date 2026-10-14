// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useEffect, useState } from "react";
import { listDeployments } from "@/lib/api/deployment";
import { listReleases } from "@/lib/api/release";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { CommitVersion__Output } from "@/lib/generated/protobuf/superverse/v2/CommitVersion";
import type { DeploymentSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/DeploymentSnapshot";
import type { ReleaseBuildRevisionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseBuildRevisionSnapshot";
import type { ReleaseSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseSnapshot";
import { useResourceWatch } from "@/lib/project/use-resource-watch";
import { timestampToISO } from "@/lib/shared/protobuf";
import { publishOutcome, type PublishFacts, type PublishKind } from "./publish-stages";

/** How many of the newest Releases and deployments the search for the latest publish reads. */
const PAGE_SIZE = 50;

/** The publish a page names: the build a publish started, or a deployment of an existing build. */
export type PublishFocus = { releaseId: string; buildRevisionId: string } | { deploymentId: string };

/** A publish's identity, with the snapshots its search read; a named publish has none yet. */
interface Found {
  releaseId: string;
  buildRevisionId: string;
  deploymentId: string;
  release: ReleaseSnapshot__Output | null;
  deployment: DeploymentSnapshot__Output | null;
}

const began = (value: { createdAt: Parameters<typeof timestampToISO>[0] } | null | undefined) =>
  value ? Date.parse(timestampToISO(value.createdAt) ?? "") || 0 : -1;

/**
 * The newest publish among the newest Releases and deployments: the newest build that deploys
 * automatically, or a deployment no listed build started (a rollback) when that began later.
 */
async function newestPublish(projectId: string, signal: AbortSignal): Promise<Found | null> {
  const [releases, deployments] = await Promise.all([
    listReleases({ projectId, pageSize: PAGE_SIZE }, signal),
    listDeployments({ projectId, pageSize: PAGE_SIZE }, signal),
  ]);
  const builds = releases.releases.flatMap((release) =>
    release.buildRevisions.filter((build) => build.autoDeploy).map((build) => ({ release, build })),
  );
  const newest = builds.toSorted((a, b) => began(b.build) - began(a.build))[0];
  const started = new Set(builds.map(({ build }) => build.autoDeploymentId).filter(Boolean));
  const rollback = deployments.deployments
    .filter((item) => !started.has(item.deploymentId))
    .toSorted((a, b) => began(b) - began(a))[0];
  if (rollback && began(rollback) > began(newest?.build))
    return {
      releaseId: "",
      buildRevisionId: "",
      deploymentId: rollback.deploymentId,
      release: null,
      deployment: rollback,
    };
  if (!newest) return null;
  const deploymentId = newest.build.autoDeploymentId;
  return {
    releaseId: newest.release.releaseId,
    buildRevisionId: newest.build.buildRevisionId,
    deploymentId,
    release: newest.release,
    deployment: deployments.deployments.find((item) => item.deploymentId === deploymentId) ?? null,
  };
}

/** The publish `focus` names, before any snapshot of it is read. */
function named(focus: PublishFocus): Found {
  return {
    releaseId: "releaseId" in focus ? focus.releaseId : "",
    buildRevisionId: "buildRevisionId" in focus ? focus.buildRevisionId : "",
    deploymentId: "deploymentId" in focus ? focus.deploymentId : "",
    release: null,
    deployment: null,
  };
}

const SETTLED_DEPLOYMENT = new Set<DeploymentSnapshot__Output["status"]>([
  "DEPLOYMENT_STATUS_READY",
  "DEPLOYMENT_STATUS_FAILED",
  "DEPLOYMENT_STATUS_CANCELLED",
  "DEPLOYMENT_STATUS_REMOVED",
  "DEPLOYMENT_STATUS_RETIRING",
  "DEPLOYMENT_STATUS_RETIRED",
]);

/** Whether the build has nothing more to report: it ended, and its automatic deployment was decided. */
function buildSettled(build: ReleaseBuildRevisionSnapshot__Output | null): boolean {
  if (!build) return false;
  if (build.status === "RELEASE_STATUS_FAILED" || build.status === "RELEASE_STATUS_CANCELLED") return true;
  return build.status === "RELEASE_STATUS_READY" && build.autoDeployStatus !== "AUTO_DEPLOY_STATUS_PENDING";
}

/**
 * The publish in progress, or the last one: the one `focus` names, or else the newest publish, searched
 * again whenever `reloadKey` changes. Its Release and Deployment Watches follow it while it runs and close
 * once it settles, keeping its last snapshots. Without a Project nothing is read.
 */
export function usePublishProgress(projectId: string | null, focus: PublishFocus | null, reloadKey: string) {
  const focusKey = focus ? JSON.stringify(focus) : "";
  const key = JSON.stringify([projectId, focusKey, focusKey ? "" : reloadKey]);
  const [search, setSearch] = useState<{ key: string; found: Found | null; error: string } | null>(null);
  const [seen, setSeen] = useState<{
    key: string;
    release?: ReleaseSnapshot__Output;
    deployment?: DeploymentSnapshot__Output;
  } | null>(null);

  useEffect(() => {
    if (projectId === null || focusKey) return;
    const controller = new AbortController();
    newestPublish(projectId, controller.signal).then(
      (found) => !controller.signal.aborted && setSearch({ key, found, error: "" }),
      (reason: unknown) =>
        !controller.signal.aborted &&
        setSearch((was) => ({
          key,
          found: was?.found ?? null,
          error: errorMessage(reason, "Could not read the publishing"),
        })),
    );
    return () => controller.abort();
  }, [focusKey, key, projectId]);

  const found = projectId === null ? null : focus ? named(focus) : search?.key === key ? search.found : null;
  const own = seen?.key === key ? seen : null;
  const release = own?.release ?? found?.release ?? null;
  const build = release?.buildRevisions.find((item) => item.buildRevisionId === found?.buildRevisionId) ?? null;
  const deploymentId =
    found?.deploymentId || (build?.autoDeployStatus === "AUTO_DEPLOY_STATUS_ACCEPTED" ? build.autoDeploymentId : "");
  const latest = own?.deployment ?? found?.deployment ?? null;
  const deployment = deploymentId && latest?.deploymentId === deploymentId ? latest : null;

  const keepRelease = useCallback(
    (snapshot: ReleaseSnapshot__Output) =>
      setSeen((was) => ({ ...(was?.key === key ? was : { key }), release: snapshot })),
    [key],
  );
  const keepDeployment = useCallback(
    (snapshot: DeploymentSnapshot__Output) =>
      setSeen((was) => ({ ...(was?.key === key ? was : { key }), deployment: snapshot })),
    [key],
  );
  const releaseId = found?.releaseId ?? "";
  const releaseWatch = useResourceWatch(
    "release",
    projectId ?? "",
    releaseId && !buildSettled(build) ? releaseId : null,
    keepRelease,
  );
  const deploymentWatch = useResourceWatch(
    "deployment",
    projectId ?? "",
    deploymentId && !(deployment && SETTLED_DEPLOYMENT.has(deployment.status)) ? deploymentId : null,
    keepDeployment,
  );

  const kind: PublishKind = !found?.buildRevisionId
    ? "rollback"
    : build?.previewValidation?.kind === "KIND_LIVE_CODE"
      ? "settings"
      : "version";
  const facts: PublishFacts | null = found ? { kind, build, deployment } : null;
  const version: CommitVersion__Output | null = release?.preparedSource?.version ?? deployment?.version ?? null;
  return {
    /** Whether the search answered; a found publish may still wait for its first snapshots. */
    loaded: projectId !== null && (Boolean(focus) || search?.key === key),
    error: search?.key === key ? search.error : "",
    facts,
    outcome: facts ? publishOutcome(facts) : null,
    /** The Release (its commit) of the publish; a rollback's comes from its deployment. */
    releaseId: releaseId || deployment?.releaseId || "",
    version,
    /** When the publish began: its build, or a rollback's deployment. */
    startedAt: timestampToISO(build?.createdAt ?? deployment?.createdAt) ?? null,
    disconnected: Boolean(releaseWatch?.disconnected || deploymentWatch?.disconnected),
  };
}

export type PublishProgress = ReturnType<typeof usePublishProgress>;
