// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * The stand-in application that mock Preview and Live URLs point at. It echoes
 * nothing from the request into the page.
 */
export function mockAppResponse(request: Request): Response {
  const env = new URL(request.url).searchParams.get("env") === "live" ? "live" : "preview";
  const html = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>Dex AI Platform mock app</title>
    <style>
      body { font-family: Geist, Helvetica, Arial, sans-serif; margin: 2rem; color: #183c25; background: #f8fbf5; }
      .card { max-width: 40rem; padding: 1.5rem; border: 1px solid #c5d6c0; border-radius: 12px; background: white; }
      code { background: #eef4e8; padding: 0.1rem 0.35rem; border-radius: 4px; }
    </style>
  </head>
  <body>
    <div class="card">
      <h1>Mock application</h1>
      <p>This page is served by Studio local mock for <code>${env}</code> URL probes.</p>
    </div>
  </body>
</html>`;
  return new Response(html, {
    status: 200,
    headers: {
      "content-type": "text/html; charset=utf-8",
      "cache-control": "no-store",
    },
  });
}
