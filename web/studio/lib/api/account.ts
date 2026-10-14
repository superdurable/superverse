// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { newRequestId } from "./transport/api-error";
import { postJson } from "./transport/unary";

// Browser client of AccountService, reached only through /api/auth: the only writer of the login cookie.

type Credentials = { email: string; password: string };

/** Registers an account and signs it in. The Idempotency-Key becomes the RegisterAccount request ID. */
export async function registerAccount(credentials: Credentials): Promise<void> {
  await postJson("/api/auth/register", credentials, { headers: { "idempotency-key": newRequestId() } });
}

export async function login(credentials: Credentials): Promise<void> {
  await postJson("/api/auth/login", credentials);
}

export async function logout(): Promise<void> {
  await postJson("/api/auth/logout", {});
}
