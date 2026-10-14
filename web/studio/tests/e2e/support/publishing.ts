// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Locator, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { DeploymentSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/DeploymentSnapshot";
import type { DeploymentStatus__Output } from "@/lib/generated/protobuf/superverse/v2/DeploymentStatus";
import type { ReleaseBuildRevisionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseBuildRevisionSnapshot";
import type { ReleaseSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseSnapshot";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { fillTemplateSummaryConnection, type SetupFill } from "./configuration";
import { exerciseTextSummaryInApplication, exerciseTextSummaryInStudio } from "./flow-run";
import { expectPhaseTimings, readProject, readSession } from "./project";
import { rpc, rpcAnswered, rpcResult, UUID } from "./studio-api";

async function readRelease(page: Page, projectId: string, releaseId: string) {
  return rpcResult(page.request, "GetRelease", { projectId, releaseId });
}

const FAILED_DEPLOYMENT_STATUSES: ReadonlySet<DeploymentStatus__Output> = new Set([
  "DEPLOYMENT_STATUS_FAILED",
  "DEPLOYMENT_STATUS_REMOVED",
]);

/**
 * How a publish runs: a version through the bar's Publish popover on the Preview tab, or Live's own
 * version again with its waiting settings, the popover on the Live tab.
 */
export type PublishMode = "version" | "configuration";

export interface PublishOptions {
  mode?: PublishMode;
  /** The version to publish; the Session's head by default. Ignored for a configuration-only change. */
  commit?: string;
  /**
   * The reason sent when the version is not in the Preview record. Without one, the version must have
   * run on Preview, and the build must record that validation.
   */
  skipReason?: string;
  /** Checks the popover's consequences before Publish is pressed. */
  checkConsequences?: (popover: Locator) => Promise<void>;
}

/** The popover's Publish button: "Publish V13", or on Live's own version "Publish this change" or "Publish V12 again". */
export function publishButton(popover: Locator): Locator {
  return popover.getByRole("button", { name: /^(Publish V\d+( again)?|Publish this change|Publish these changes)$/ });
}

/** Opens the bar's Publish popover on the Preview tab, or on the Live tab for Live's own version. */
export async function openPublishPopover(page: Page, projectId: string, tab: "preview" | "live" = "preview") {
  await page.goto(`/p/${projectId}/flow${tab === "live" ? "?v=live" : ""}`);
  const button = page.getByTestId("publish-to-live");
  await expect(button).toBeEnabled({ timeout: 60_000 });
  await button.click();
  const popover = page.getByRole("dialog", { name: "Publish to Live" });
  await expect(popover).toBeVisible();
  return popover;
}

/**
 * Chooses `commit` among the versions ahead of Live, unfolding the others, checks its Preview validation
 * and writes the reason a version not in the Preview record needs. Answers whether Preview validated it.
 */
export async function chooseVersion(popover: Locator, commit: string, skipReason: string | undefined) {
  const version = popover.getByTestId(`publish-version-${commit}`);
  await expect(popover.getByRole("radiogroup")).toBeVisible({ timeout: 60_000 });
  if (!(await version.isVisible())) await popover.getByRole("button", { name: /^Another version/ }).click();
  await version.click();
  await expect(version).toHaveAttribute("aria-checked", "true");
  await expect(version).toContainText(/^V\d+/);
  await expect(version).toContainText(/ran on Preview at|not in the Preview record/);
  if (!skipReason) await expect(version).toContainText("ran on Preview at", { timeout: 60_000 });
  const validated = (await version.textContent())?.includes("ran on Preview at") ?? false;
  if (!validated) {
    if (!skipReason) throw new Error(`Version ${commit} is not in the Preview record, and no skip reason was given`);
    await expect(publishButton(popover), "a version not in the Preview record needs a reason").toBeDisabled();
    await popover.getByLabel(/Why publish it without a Preview run\?/).fill(skipReason);
  }
  return validated;
}

/**
 * Presses Publish once the popover enables it (the version's Release prepared and its next publish
 * READY), which builds the frozen copy from exactly the next publish shown and starts its build. Answers
 * the DeriveLiveConfiguration and StartReleaseBuild responses and the revisions the popover showed; Tools,
 * Publishing then follows the build.
 */
export async function publishFromPopover(page: Page, popover: Locator) {
  const publish = publishButton(popover);
  // The popover prepares a version's Release when it has none, which takes minutes.
  await expect(publish).toBeEnabled({ timeout: 10 * 60_000 });
  const shown = (await publish.getAttribute("data-revision")) ?? "";
  const shownPreview = (await publish.getAttribute("data-preview-revision")) ?? "";
  const [copied, response] = await Promise.all([
    page.waitForResponse(rpcAnswered("DeriveLiveConfiguration")),
    page.waitForResponse(rpcAnswered("StartReleaseBuild")),
    publish.click(),
  ]);
  await expect(page).toHaveURL(/[?&]tool=publishing(&|$)/, { timeout: 30_000 });
  await expect(page.getByTestId("publish-progress")).toBeVisible({ timeout: 60_000 });
  return { copied, response, shown, shownPreview };
}

/** Opens the popover on Live's own version from the Live tab's banner, or from the bar when nothing waits. */
async function openOnLiveVersion(page: Page, projectId: string, liveReleaseId: string) {
  const target = {
    projectId,
    scope: "PROJECT_SCOPE_LIVE" as const,
    releaseId: liveReleaseId,
    nextPublish: true as const,
  };
  const { configuration } = await rpcResult(page.request, "GetProjectConfiguration", { target });
  const waiting = configuration?.items.filter((item) => item.differsFromLive).length ?? 0;
  // The banner publishes Live's version with what waits, or again with the settings it runs.
  const banner = page.getByTestId("live-banner");
  await expect(banner.getByTestId("live-waiting-count")).toHaveText(
    waiting === 0 ? "Nothing waiting for a publish" : `${waiting} ${waiting === 1 ? "change" : "changes"} waiting`,
    { timeout: 60_000 },
  );
  await banner.getByTestId("change-live-configuration").click();
  const popover = page.getByRole("dialog", { name: "Publish to Live" });
  await expect(popover).toBeVisible();
  return popover;
}

export async function publishLive(
  page: Page,
  projectId: string,
  fillConnections?: SetupFill,
  { mode = "version", commit, skipReason, checkConsequences }: PublishOptions = {},
) {
  const before = await readProject(page, projectId);
  let validated = false;
  let target: string;
  let popover: Locator;
  if (mode === "configuration") {
    if (!before.live) throw new Error("A configuration-only change needs a Live version");
    target = before.live.releaseId;
    // The change is made on Setup's Live tab, whose banner counts it as waiting and opens the popover on it.
    await page.goto(`/p/${projectId}/tools?tool=setup&v=live`);
    await expect(page.getByTestId("setup-checklist")).toBeVisible({ timeout: 10 * 60_000 });
    if (fillConnections) await fillConnections(page, "next");
    popover = await openOnLiveVersion(page, projectId, target);
    await expect(popover).toContainText(/again with \d+ setting changes?\.|has no waiting changes/, {
      timeout: 3 * 60_000,
    });
  } else {
    // Live's next publish is set up in Setup's Next publish column; the popover only links to what is missing.
    if (fillConnections) {
      await page.goto(`/p/${projectId}/tools?tool=setup`);
      await expect(page.getByTestId("setup-checklist")).toBeVisible({ timeout: 10 * 60_000 });
      await fillConnections(page, "next");
    }
    target = commit ?? (await readSession(page, projectId)).headCommit;
    popover = await openPublishPopover(page, projectId);
    validated = await chooseVersion(popover, target, skipReason);
  }
  await expect(popover.getByTestId("publish-consequences")).toBeVisible({ timeout: 10 * 60_000 });
  await expect(popover.getByTestId("publish-replaces")).toContainText(
    mode === "configuration"
      ? /rebuilds with the (new|same) settings/
      : before.live
        ? /^Replaces V\d+\./
        : "for the first time",
  );
  if (checkConsequences) await checkConsequences(popover);
  const { copied, response: releaseResponse, shown, shownPreview } = await publishFromPopover(page, popover);
  expect(releaseResponse.ok()).toBe(true);
  const acceptedBuild: RpcOutput<"StartReleaseBuild"> = await releaseResponse.json();
  expect(Object.keys(acceptedBuild)).toEqual(["buildRevisionId"]);
  const requestedBuild: RpcInput<"StartReleaseBuild"> = releaseResponse.request().postDataJSON();
  expect(requestedBuild.projectId).toBe(projectId);
  expect(requestedBuild.requestId).toMatch(UUID);
  // A Release ID is its source commit.
  const releaseId = String(requestedBuild.releaseId);
  expect(releaseId).toBe(target);
  expect(acceptedBuild.buildRevisionId).toBe(requestedBuild.buildRevisionId);
  expect(acceptedBuild.buildRevisionId).toMatch(/^build-[0-9a-f-]{36}$/);
  // The copy is built from exactly the next publish the popover showed, and the build freezes that copy.
  expect(copied.ok()).toBe(true);
  const copyRequest: RpcInput<"DeriveLiveConfiguration"> = copied.request().postDataJSON();
  expect(String(copyRequest.expectedRevision)).toBe(shown);
  expect(String(copyRequest.expectedPreviewConfigurationRevision)).toBe(shownPreview);
  const copy: RpcOutput<"DeriveLiveConfiguration"> = await copied.json();
  expect(String(requestedBuild.configurationRevision)).toBe(copy.configuration?.revision);
  // Publish deploys automatically, fenced on the Live revision the page showed.
  expect(Boolean(requestedBuild.autoDeploy)).toBe(true);
  expect(String(requestedBuild.expectedLiveRevision)).toBe(before.live?.revision ?? "0");
  expect(requestedBuild.previewSkipReason ?? "").toBe(mode === "version" && !validated ? (skipReason ?? "") : "");
  await expect(page).toHaveURL(new RegExp(`[?&]build=${acceptedBuild.buildRevisionId}(&|$)`));
  await expect
    .poll(
      async () => {
        const state = (await readRelease(page, projectId, releaseId)).release;
        const build = state?.buildRevisions.find((item) => item.buildRevisionId === acceptedBuild.buildRevisionId);
        if (build?.status === "RELEASE_STATUS_FAILED") throw new Error(`Release build failed: ${build.failure?.code}`);
        return (
          state?.readyBuildRevisionId === acceptedBuild.buildRevisionId && build?.status === "RELEASE_STATUS_READY"
        );
      },
      { timeout: 10 * 60_000, intervals: [1000, 2000] },
    )
    .toBe(true);
  const details = await readRelease(page, projectId, releaseId);
  const release = details.release;
  if (!release) throw new Error("READY Release is unavailable");
  expect(release.projectId).toBe(projectId);
  expect(release.releaseId).toBe(releaseId);
  expect(release.status).toBe("RELEASE_STATUS_READY");
  expect(release.sourceCommit).toBe(releaseId);
  expect(details.flowDefinitions.length, "READY Release must include its immutable Flow Definitions").toBeGreaterThan(
    0,
  );
  for (const definition of details.flowDefinitions) {
    expect(definition.valid).toBe(true);
    expect(definition.digest).toMatch(/^sha256:[0-9a-f]{64}$/);
  }
  const build = release.buildRevisions.find((item) => item.buildRevisionId === acceptedBuild.buildRevisionId);
  if (!build || build.status !== "RELEASE_STATUS_READY")
    throw new Error("Exact immutable Release build is unavailable");
  expect(build.imageDigest).toMatch(/^sha256:[0-9a-f]{64}$/);
  expect(build.draftConfigurationRevision).toBe(copy.configuration?.revision);
  // The Flow measures the configuration freeze and the artifact publication around the builder's own phases.
  expectPhaseTimings(build.phaseTimings, ["CONFIGURATION_FREEZE", "ARTIFACT_PUBLICATION"]);
  expect(build.phaseTimings.length, "the builder's phases lie between the Flow's two").toBeGreaterThan(2);
  expectPhaseTimings(release.preparedSource?.phaseTimings ?? [], []);
  expect(copy.configuration?.releaseId).toBe(release.releaseId);
  // The build records how it met the publishing rule.
  expect(build.previewValidation?.kind).toBe(
    mode === "configuration" ? "KIND_LIVE_CODE" : validated ? "KIND_VALIDATED" : "KIND_SKIPPED",
  );
  const published = await awaitPublished(page, projectId, release, build);
  await expectInvalidDeploymentIdentitiesRejected(page, projectId, release, build);
  return published;
}

/** A deployment whose identity is not a DNS label is refused at the actual BFF/gRPC boundary. */
async function expectInvalidDeploymentIdentitiesRejected(
  page: Page,
  projectId: string,
  release: ReleaseSnapshot__Output,
  build: ReleaseBuildRevisionSnapshot__Output,
) {
  const current = await readProject(page, projectId);
  for (const deploymentId of ["a".repeat(37), "bad.id", "bad_id", "-bad", "bad-"]) {
    const rejected = await rpc(
      page.request,
      "StartDeployment",
      {
        projectId,
        deploymentId,
        requestId: randomUUID(),
        releaseId: release.releaseId,
        buildRevisionId: build.buildRevisionId,
        configurationRevision: build.configurationRevision,
        expectedLiveRevision: current.live?.revision ?? "0",
      },
      { timeout: 30_000 },
    );
    expect(rejected.status(), "Invalid deployment identity must be rejected at the actual BFF/gRPC boundary").toBe(400);
  }
}

/**
 * Waits until the automatic deployment that the popover's build revision started serves Live, and
 * requires Tools, Publishing's progress to say so.
 */
async function awaitPublished(
  page: Page,
  projectId: string,
  release: ReleaseSnapshot__Output,
  build: ReleaseBuildRevisionSnapshot__Output,
) {
  let deploymentId = "";
  await expect
    .poll(
      async () => {
        const state = (await readRelease(page, projectId, release.releaseId)).release;
        const current = state?.buildRevisions.find((item) => item.buildRevisionId === build.buildRevisionId);
        if (current?.autoDeployStatus === "AUTO_DEPLOY_STATUS_REJECTED")
          throw new Error(`The automatic deployment was rejected: ${current.autoDeployFailure?.code}`);
        deploymentId = current?.autoDeploymentId ?? "";
        return current?.autoDeployStatus === "AUTO_DEPLOY_STATUS_ACCEPTED" && deploymentId !== "";
      },
      { timeout: 5 * 60_000, intervals: [1000, 2000] },
    )
    .toBe(true);
  let deployment: DeploymentSnapshot__Output | undefined;
  await expect
    .poll(
      async () => {
        deployment =
          (await rpcResult(page.request, "GetDeployment", { projectId, deploymentId })).deployment ?? undefined;
        if (deployment && FAILED_DEPLOYMENT_STATUSES.has(deployment.status))
          throw new Error(`Live deployment ${deploymentId} reached ${deployment.status}`);
        const pointer = (await readProject(page, projectId)).live;
        return (
          deployment?.status === "DEPLOYMENT_STATUS_READY" &&
          pointer?.isRemoving === false &&
          pointer.deploymentId === deploymentId &&
          pointer.releaseId === release.releaseId &&
          pointer.revision === deployment.generation
        );
      },
      { timeout: 10 * 60_000, intervals: [1000, 2000] },
    )
    .toBe(true);
  if (!deployment) throw new Error("The published deployment is unavailable");
  expect(deployment.buildRevisionId).toBe(build.buildRevisionId);
  // The deployment runs the configuration the build froze, as a deployment from the panel does.
  expect(deployment.configurationRevision).toBe(build.configurationRevision);
  expectPhaseTimings(deployment.phaseTimings, ["VerifyReadiness", "ReleaseProjectDeployment"]);
  // Tools, Publishing's stage bar ends at Go live, and what is Live opens the new address.
  const progress = page.getByTestId("publish-progress");
  await expect(progress).toHaveAttribute("data-outcome", "published", { timeout: 60_000 });
  await expect(progress.getByTestId("publish-stages")).toHaveAttribute("aria-valuetext", /^(\d) of \1 stages done$/);
  await expect(page.getByRole("region", { name: "What is Live" }).getByRole("link", { name: "Open" })).toHaveAttribute(
    "href",
    deployment.url,
  );
  return { url: deployment.url, deployment, release, imageDigest: build.imageDigest };
}

export async function removeLive(page: Page, projectId: string, releaseId: string) {
  await page.goto(`/p/${projectId}/tools?tool=publishing`);
  await expect(page.getByTestId("live-serving-state")).toHaveText("READY");
  const previous = (await readProject(page, projectId)).live;
  expect(previous?.isRemoving).toBe(false);
  expect(previous?.releaseId).toBe(releaseId);
  if (!previous?.deploymentId) throw new Error("Live removal requires the observed READY deployment");
  await page.getByRole("button", { name: "Stop Live…", exact: true }).click({ timeout: 30_000 });
  // Stop Live states what stops and what is kept before anything happens.
  const dialog = page.getByTestId("stop-live-dialog");
  await expect(dialog.getByTestId("stop-live-consequences")).toContainText("Its triggers stop");
  const responsePromise = page.waitForResponse(rpcAnswered("RemoveLive"));
  await dialog.getByRole("button", { name: "Stop Live", exact: true }).click();
  const response = await responsePromise;
  // Removal is fenced on the Live pointer the page showed.
  expect(response.request().postDataJSON()).toEqual({
    projectId,
    requestId: expect.stringMatching(UUID),
    deploymentId: previous.deploymentId,
    expectedLiveRevision: previous.revision,
  });
  expect(response.ok()).toBe(true);
  const { project: accepted }: RpcOutput<"RemoveLive"> = await response.json();
  expect(accepted?.projectId).toBe(projectId);
  await expect.poll(async () => (await readProject(page, projectId)).live, { timeout: 5 * 60_000 }).toBeNull();
  const oldGate = await page.request.get(
    `/dex/projects/${projectId}/environments/live/releases/${releaseId}/api/v2/catalog`,
    { headers: { "x-superverse-deployment-id": previous.deploymentId } },
  );
  expect(
    oldGate.status(),
    "The previously observed deployment must be rejected by current Live authorization after removal",
  ).toBe(409);
}
/** Publishes the Session's pushed head to Live and checks the same heading and summary Flow there. */
export async function publishTemplateEditLive(
  page: Page,
  app: Page,
  projectId: string,
  marker: string,
  headCommit: string,
  mode: PublishMode = "version",
) {
  const live = await publishLive(page, projectId, fillTemplateSummaryConnection, { mode });
  expect(live.release.sourceCommit).toBe(headCommit);
  await app.goto(live.url);
  await expect(app.getByRole("heading", { name: marker, exact: true })).toBeVisible();
  await exerciseTextSummaryInApplication(app);
  await exerciseTextSummaryInStudio(page, projectId, "live", live.release.releaseId);
  return live;
}
