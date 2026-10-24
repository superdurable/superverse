// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { headers } from "next/headers";
import { AccountForm } from "@/components/shell/AccountForm";
import { BrandMark } from "@/components/shell/BrandMark";
import { callerFromCookieHeader } from "@/lib/server/caller-envelope";
import { callAccountRpc } from "@/lib/server/grpc-client";
import { authConfig } from "@/lib/server/auth-config";
import { openCookieHeader, sessionCSRF } from "@/lib/server/login-host/browser-cookie";
import { readLoginContext } from "@/lib/server/login-host/protocol";
import { issuerSecret } from "@/lib/server/login-host/secrets";
import { LoginHostActions } from "./LoginHostActions";

export async function LoginHostEntry({ mode }: { mode: "login" | "register" }) {
  const config = authConfig();
  const requestHeaders = await headers();
  const secure = config.loginOrigin.startsWith("https:");
  let context;
  try {
    context = readLoginContext(openCookieHeader("login", requestHeaders.get("cookie"), secure));
  } catch {
    context = null;
  }
  const caller = callerFromCookieHeader(requestHeaders.get("cookie"), "login");
  let account;
  if (caller) {
    try {
      ({ account } = await callAccountRpc("GetCurrentAccount", {}, caller, "login", issuerSecret()));
    } catch (error) {
      if (!(error instanceof Error && "status" in error && error.status === 401)) throw error;
    }
  }
  if (!context)
    return (
      <main className="grid h-full bg-bg p-6">
        <section className="m-auto w-full max-w-md rounded-xl border border-line-soft bg-panel p-8 shadow-(--shadow-lift)">
          <BrandMark />
          <h1 className="mt-6 text-2xl font-semibold">Choose a product</h1>
          <p className="mt-2 text-sm text-ink-dim">Sign in to the product you want to use.</p>
          {account && caller ? (
            <LoginHostActions
              email={account.email}
              logoutCSRF={sessionCSRF(caller.sessionToken)}
              productOrigin={config.productOrigin}
            />
          ) : (
            <a
              className="mt-6 block rounded-md bg-accent px-4 py-2 text-center font-medium text-white"
              rel="noreferrer"
              href={`${config.productOrigin}/api/auth/start`}
            >
              Dex AI Platform
            </a>
          )}
          <p className="mt-4 text-sm text-ink-dim">Managed Dex Engine — Coming soon</p>
        </section>
      </main>
    );
  if (account && caller)
    return (
      <main className="grid h-full bg-bg p-6">
        <section className="m-auto w-full max-w-md rounded-xl border border-line-soft bg-panel p-8 shadow-(--shadow-lift)">
          <BrandMark />
          <h1 className="mt-6 text-2xl font-semibold">Continue to Dex AI Platform</h1>
          <LoginHostActions
            email={account.email}
            csrf={context.csrf}
            logoutCSRF={sessionCSRF(caller.sessionToken)}
            productOrigin={config.productOrigin}
          />
        </section>
      </main>
    );
  return <AccountForm mode={mode} loginHost csrfToken={context.csrf} />;
}
