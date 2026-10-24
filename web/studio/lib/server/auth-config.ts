// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { httpError } from "./request-body";

export interface AuthConfig {
  productOrigin: string;
  loginOrigin: string;
  handoffEnabled: boolean;
  exchangeEnabled: boolean;
  exchangeURL: string;
  studioClientID: "studio";
}

let loaded: AuthConfig | undefined;

function exactOrigin(value: unknown): value is string {
  if (typeof value !== "string" || value.length > 512 || /[%\\\u0000-\u001f\u007f]/.test(value)) return false;
  try {
    const url = new URL(value);
    return (
      url.origin === value &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      /^[a-z0-9.-]+$/.test(url.hostname) &&
      (url.protocol === "https:" ||
        (url.protocol === "http:" && (url.hostname === "localhost" || url.hostname.endsWith(".localhost"))))
    );
  } catch {
    return false;
  }
}

function exactExchangeOrigin(value: unknown): value is string {
  if (typeof value !== "string" || value.length > 512) return false;
  try {
    const url = new URL(value);
    return (
      url.origin === value &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      /^[a-z0-9.-]+$/.test(url.hostname) &&
      (url.protocol === "https:" || url.protocol === "http:")
    );
  } catch {
    return false;
  }
}

/** A validated subset of the strict server profile, rendered into Studio's process environment. */
export function authConfig(): AuthConfig {
  if (loaded) return loaded;
  const raw = process.env.SUPERVERSE_STUDIO_AUTH_CONFIG;
  if (!raw) {
    const productOrigin = process.env.SUPERVERSE_STUDIO_PUBLIC_ORIGIN ?? "";
    if (!exactOrigin(productOrigin)) throw httpError(503, "Studio origin is unavailable");
    loaded = {
      productOrigin,
      loginOrigin: "",
      handoffEnabled: false,
      exchangeEnabled: false,
      exchangeURL: "",
      studioClientID: "studio",
    };
    return loaded;
  }
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    throw httpError(503, "Invalid Studio auth configuration");
  }
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw httpError(503, "Invalid Studio auth configuration");
  const item = value as Record<string, unknown>;
  if (
    Object.keys(item).sort().join(",") !==
      "exchangeEnabled,exchangeURL,handoffEnabled,loginOrigin,productOrigin,studioClientID" ||
    !exactOrigin(item.productOrigin) ||
    typeof item.handoffEnabled !== "boolean" ||
    typeof item.exchangeEnabled !== "boolean" ||
    item.studioClientID !== "studio" ||
    (item.handoffEnabled && !item.exchangeEnabled) ||
    ((item.handoffEnabled || item.exchangeEnabled) &&
      (!exactOrigin(item.loginOrigin) || item.loginOrigin === item.productOrigin)) ||
    typeof item.exchangeURL !== "string" ||
    (item.exchangeEnabled && !exactExchangeOrigin(item.exchangeURL)) ||
    item.productOrigin !== process.env.SUPERVERSE_STUDIO_PUBLIC_ORIGIN
  )
    throw httpError(503, "Invalid Studio auth configuration");
  loaded = item as unknown as AuthConfig;
  return loaded;
}

export function originHost(origin: string): string {
  return new URL(origin).host;
}
