// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, test } from "@/tests/e2e/support/coverage/spec-coverage";
import { matching } from "@/tests/e2e/support/json-assert";
import { ownerFixture } from "@/tests/e2e/support/private-files";
import { rpcPosted } from "@/tests/e2e/support/studio-api";

// Read-only observation of work already started through real services.
// No provider or resource mutation is admitted by this scenario.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("publishing reattaches to exact Release and active Deployment after reload", async ({ page }, testInfo) => {
  const filename = process.env.SUPERVERSE_E2E_PUBLISHING_FIXTURE;
  if (!filename)
    throw new Error(
      "Requires private SUPERVERSE_E2E_PUBLISHING_FIXTURE with owner email/password, projectId, sourceCommit, releaseId and a currently active deploymentId",
    );
  const owner = ownerFixture(filename, "Publishing fixture");
  const projectId = matching(owner.projectId, /^[a-z0-9]+$/, "Publishing fixture projectId");
  matching(owner.sourceCommit, /^[a-f0-9]{40}$/, "Publishing fixture sourceCommit");
  const releaseId = matching(owner.releaseId, /^[a-zA-Z0-9_-]+$/, "Publishing fixture releaseId");
  const deploymentId = matching(owner.deploymentId, /^[a-zA-Z0-9_-]+$/, "Publishing fixture deploymentId");
  const origin = new URL(process.env.SUPERVERSE_E2E_BASE_URL!).origin;
  expect(
    (
      await page.request.post("/api/auth/login", {
        headers: { origin },
        data: { email: owner.email, password: owner.password },
      })
    ).status(),
  ).toBe(200);
  // Each Watch the page opens, as its method and the resource it names.
  const releaseWatch = `WatchRelease:${releaseId}`;
  const deploymentWatch = `WatchDeployment:${deploymentId}`;
  const watches: string[] = [];
  const listingLimits: number[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    const method = url.pathname.match(/^\/api\/rpc\/(Watch[A-Za-z]+)$/)?.[1];
    if (method && url.searchParams.get("projectId") === projectId)
      watches.push(`${method}:${url.searchParams.get("releaseId") ?? url.searchParams.get("deploymentId") ?? ""}`);
    if (rpcPosted("ListReleases")(request)) listingLimits.push(Number(request.postDataJSON().pageSize));
  });
  try {
    await page.goto(
      `/p/${projectId}/tools?tool=publishing&commit=${releaseId}&role=viewer&hasLive=false&readOnly=true`,
    );
    await expect(page.getByRole("heading", { name: "Publishing", exact: true })).toBeVisible();
    const firstProject = await page.evaluate(
      async (path) =>
        new Promise<{ activeDeployment?: { deploymentId: string } }>((resolve, reject) => {
          const source = new EventSource(path);
          const deadline = setTimeout(() => {
            source.close();
            reject(new Error("Real Project Watch did not return a snapshot"));
          }, 15000);
          source.addEventListener(
            "snapshot",
            (event) => {
              clearTimeout(deadline);
              source.close();
              resolve(JSON.parse(event.data).snapshot);
            },
            { once: true },
          );
          source.onerror = () => {
            clearTimeout(deadline);
            source.close();
            reject(new Error("Real Project Watch failed"));
          };
        }),
      `/api/rpc/WatchProject?${new URLSearchParams({ projectId })}`,
    );
    expect(
      firstProject.activeDeployment?.deploymentId,
      "Fixture must identify the actual active deployment lease, not the Live pointer",
    ).toBe(deploymentId);
    await expect(page.getByRole("list", { name: "Versions", exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "History", exact: true })).toBeVisible();
    await expect.poll(() => watches.includes(releaseWatch)).toBe(true);
    await expect.poll(() => watches.includes(deploymentWatch)).toBe(true);
    const before = watches.filter((watch) => watch === deploymentWatch).length;
    await page.reload();
    await expect(page.getByRole("region", { name: "History", exact: true })).toBeVisible();
    await expect.poll(() => watches.filter((watch) => watch === deploymentWatch).length).toBeGreaterThan(before);
    await expect.poll(() => listingLimits.length).toBeGreaterThan(0);
    expect(
      listingLimits.every((limit) => limit === 50),
      "Release discovery remains bounded per request",
    ).toBe(true);
    await testInfo.attach("publishing-resource-identities", {
      body: JSON.stringify({ projectId: projectId, releaseId: releaseId, deploymentId: deploymentId }),
      contentType: "application/json",
    });
  } finally {
    expect((await page.request.post("/api/auth/logout", { headers: { origin } })).status()).toBe(200);
  }
});
