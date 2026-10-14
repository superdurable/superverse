// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomBytes, scryptSync, timingSafeEqual } from "node:crypto";
import type { AccountLogin } from "@/lib/generated/protobuf/superverse/v2/AccountLogin";
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";
import type { CallerContext__Output } from "@/lib/generated/protobuf/superverse/v2/CallerContext";
import { httpError } from "@/lib/server/request-body";
import { randomHex } from "./clock";

const MOCK_ADMIN_EMAIL = "admin@superverse.test";

type MockAccounts = {
  accounts: Map<string, { salt: Buffer; hash: Buffer; account: AccountSnapshot__Output }>;
  sessions: Map<string, AccountSnapshot__Output>;
};

// In-memory accounts for mock mode only; they disappear when the dev server stops. Next loads
// route handlers and the workspace layout as separate module instances, so one set of logins
// lives on globalThis for /api/auth, /api/rpc and the layout's account read alike.
const shared = globalThis as { superverseMockAccounts?: MockAccounts };
const { accounts, sessions }: MockAccounts = (shared.superverseMockAccounts ??= {
  accounts: new Map(),
  sessions: new Map(),
});

export function mockSignIn(
  action: "register" | "login",
  credentials: { email: string; password: string },
): AccountLogin {
  const email = credentials.email.trim().toLowerCase();
  if (!email.includes("@")) throw httpError(400, "Enter a valid email address");
  const known = accounts.get(email);
  if (action === "register") {
    if (known) throw httpError(409, "Account already exists");
    const salt = randomBytes(16);
    accounts.set(email, {
      salt,
      hash: scryptSync(credentials.password, salt, 32),
      account: { accountId: randomHex(32), email, isAdmin: email === MOCK_ADMIN_EMAIL },
    });
  } else if (!known || !timingSafeEqual(known.hash, scryptSync(credentials.password, known.salt, 32))) {
    throw httpError(401, "Email or password is incorrect");
  }
  const account = accounts.get(email)!.account;
  const sessionToken = randomBytes(32).toString("base64url");
  sessions.set(sessionToken, account);
  // The login expires on the real clock, not the mock's: it becomes the cookie's Expires, which the
  // browser checks against its own time even when a mock epoch lies in the past.
  return { account, sessionToken, expiresAt: { seconds: String(Math.floor(Date.now() / 1000) + 86400), nanos: 0 } };
}

export function mockLogout(sessionToken: string): void {
  sessions.delete(sessionToken);
}

/** The caller's account, checked like Go checks it: a live login of the same account and email. */
export function mockAccount(caller: CallerContext__Output | null): AccountSnapshot__Output {
  const account = sessions.get(caller?.sessionToken ?? "");
  if (!account || account.accountId !== caller?.accountId || account.email !== caller.canonicalEmail)
    throw httpError(401, "Sign in to continue");
  return account;
}

/** The registered account with `email`, as the server resolves a member to add; null when none is. */
export function mockAccountByEmail(email: string): AccountSnapshot__Output | null {
  return accounts.get(email.trim().toLowerCase())?.account ?? null;
}
