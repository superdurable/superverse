// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { abortError } from "@/lib/shared/async";
import { readFlowApiJson } from "./api-error";

/** The identities of the instance the page observed, sent with every request as concurrency selectors. */
export interface ObservedFlowTarget {
  deploymentId?: string;
  workspaceRequestId?: string;
  generation?: string;
}

/** Same-origin requests to one mounted resource's Flow API; paths are relative to its scope. */
export interface FlowApiClient {
  fetch(path: string, init?: RequestInit): Promise<Response>;
}

type OwnedFlowApiClient = FlowApiClient & { activate(): void; dispose(): void };

/**
 * Each mounted resource owns its address and outstanding requests. `basePath` comes from the typed
 * Studio routes and request paths are built in code; the server resolves and authorizes each one.
 */
export function createFlowApiClient(basePath: string, observed: ObservedFlowTarget): OwnedFlowApiClient {
  let active = true;
  let csrfToken = "";
  const observedHeaders = Object.fromEntries(
    Object.entries({
      "X-Superverse-Deployment-Id": observed.deploymentId,
      "X-Superverse-Workspace-Request-Id": observed.workspaceRequestId,
      "X-Superverse-Generation": observed.generation,
    }).filter((entry): entry is [string, string] => typeof entry[1] === "string" && entry[1] !== ""),
  );
  const requests = new Set<AbortController>();
  return {
    activate() {
      active = true;
    },
    dispose() {
      active = false;
      for (const request of requests) request.abort();
      requests.clear();
    },
    async fetch(path, init) {
      if (!active) throw abortError("The selected Flow environment changed");
      if (requests.size >= 64) throw new Error("Too many active Flow API requests");
      const controller = new AbortController();
      const signals = [controller.signal, AbortSignal.timeout(35000)];
      if (init?.signal) signals.push(init.signal);
      const signal = AbortSignal.any(signals);
      requests.add(controller);
      try {
        const headers = new Headers(init?.headers);
        for (const [name, field] of Object.entries(observedHeaders)) headers.set(name, field);
        // The session-bound token from the BFF's last response header replaces any token a caller
        // copied from a response body (connector and environment writes). The /dex mock sends only
        // the body token, so there the caller's token is the one sent.
        if (csrfToken && !["GET", "HEAD"].includes(init?.method ?? "GET")) headers.set("X-Dex-CSRF-Token", csrfToken);
        const response = await fetch(`${basePath}${path}`, { ...init, headers, signal });
        // These endpoints return finite JSON payloads. Keep ownership until the
        // complete body arrives, including when the selection changes after headers.
        const body = response.body ? await response.arrayBuffer() : null;
        signal.throwIfAborted();
        const nextCSRF = response.headers.get("x-superverse-csrf-token");
        if (nextCSRF) csrfToken = nextCSRF;
        return new Response(body, {
          status: response.status,
          statusText: response.statusText,
          headers: response.headers,
        });
      } finally {
        requests.delete(controller);
      }
    },
  };
}

/** A view within an environment cancels its own work when its selected Flow changes. */
export function createFlowRequestOwner(parent: FlowApiClient, identity: string): OwnedFlowApiClient {
  let active = true;
  let controller = new AbortController();
  return {
    activate() {
      active = true;
      if (controller.signal.aborted) controller = new AbortController();
    },
    dispose() {
      active = false;
      controller.abort();
    },
    fetch(path, init) {
      if (!active) return Promise.reject(abortError(`The selected Flow ${identity} changed`));
      return parent.fetch(path, {
        ...init,
        signal: init?.signal ? AbortSignal.any([controller.signal, init.signal]) : controller.signal,
      });
    },
  };
}

/** Binds a request to the Flow definition revision its caller rendered; an empty revision binds none. */
export function definitionRevisionHeaders(revision: string): HeadersInit {
  return revision === "" ? {} : { "X-Dex-Flow-Definition-Revision": revision };
}

/**
 * Sends a JSON body, if any, and reads the JSON answer. A request bound to a definition revision is
 * rejected by the server when the Flow definition changed underneath it; `headers` carry any other
 * fence, such as a configuration write's revisions.
 */
export async function sendFlowApiJson<T>(
  client: FlowApiClient,
  path: string,
  method: "POST" | "PUT" | "PATCH" | "DELETE",
  body: unknown,
  {
    definitionRevision = "",
    headers = {},
    signal,
  }: { definitionRevision?: string; headers?: Record<string, string>; signal?: AbortSignal } = {},
): Promise<T> {
  const response = await client.fetch(path, {
    method,
    signal,
    headers: { "Content-Type": "application/json", ...definitionRevisionHeaders(definitionRevision), ...headers },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  return readFlowApiJson<T>(response);
}
