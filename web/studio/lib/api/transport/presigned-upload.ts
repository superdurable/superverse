// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { abortError } from "@/lib/shared/async";

/** A presigned PUT: the URL and the headers storage signed, which the request must carry exactly. */
export interface PresignedPut {
  url: string;
  headers: readonly { name: string; value: string }[];
}

/** Storage refused the PUT, with its HTTP status; 0 is a network failure or a refused CORS preflight. */
export class PresignedUploadError extends Error {
  constructor(readonly status: number) {
    super(status === 0 ? "The upload did not reach storage" : `Storage refused the upload with HTTP ${status}`);
    this.name = "PresignedUploadError";
  }
}

/** HTTP 412: the object of this digest already exists, as after an earlier PUT of the same bytes. */
export const UPLOAD_ALREADY_STORED = 412;

/**
 * PUTs `body` to storage with exactly the signed headers. XMLHttpRequest, unlike fetch, reports upload
 * progress (`onProgress`, 0 to 1). The browser sets Host and Content-Length, which storage checks against
 * the signed size. Resolves on 2xx; rejects with PresignedUploadError, or with an AbortError when `signal`
 * aborts, which also aborts the request.
 */
export function putPresigned(
  target: PresignedPut,
  body: Blob,
  onProgress: (fraction: number) => void,
  signal: AbortSignal,
): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) return reject(abortError("Upload canceled"));
    const request = new XMLHttpRequest();
    const abort = () => request.abort();
    const settle = () => signal.removeEventListener("abort", abort);
    request.open("PUT", target.url);
    for (const { name, value } of target.headers) request.setRequestHeader(name, value);
    request.upload.onprogress = (event) => {
      if (event.lengthComputable && event.total > 0) onProgress(event.loaded / event.total);
    };
    request.onload = () => {
      settle();
      if (request.status >= 200 && request.status < 300) resolve();
      else reject(new PresignedUploadError(request.status));
    };
    request.onerror = () => {
      settle();
      reject(new PresignedUploadError(0));
    };
    request.onabort = () => {
      settle();
      reject(abortError("Upload canceled"));
    };
    signal.addEventListener("abort", abort, { once: true });
    request.send(body);
  });
}
