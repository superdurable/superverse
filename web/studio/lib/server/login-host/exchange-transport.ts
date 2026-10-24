// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { request as httpRequest } from "node:http";
import { request as httpsRequest } from "node:https";
import { httpError } from "@/lib/server/request-body";

/** POST to a fixed private endpoint while retaining the canonical login Host. */
export function postExchange(
  endpoint: string,
  canonicalHost: string,
  authorization: string,
  body: string,
  requestSignal: AbortSignal,
): Promise<{ status: number; body: Buffer }> {
  const url = new URL("/api/auth/exchange", endpoint);
  const send = url.protocol === "https:" ? httpsRequest : httpRequest;
  const signal = AbortSignal.any([requestSignal, AbortSignal.timeout(10_000)]);
  return new Promise((resolve, reject) => {
    const request = send(
      url,
      {
        method: "POST",
        signal,
        headers: {
          host: canonicalHost,
          authorization,
          "content-type": "application/json",
          "content-length": Buffer.byteLength(body),
        },
      },
      (response) => {
        const chunks: Buffer[] = [];
        let size = 0;
        response.on("data", (chunk: Buffer) => {
          size += chunk.length;
          if (size > 4096) {
            response.destroy();
            reject(httpError(503, "Login exchange response is invalid"));
          } else chunks.push(chunk);
        });
        response.on("end", () => resolve({ status: response.statusCode ?? 503, body: Buffer.concat(chunks) }));
        response.on("error", reject);
      },
    );
    request.on("error", reject);
    request.end(body);
  });
}
