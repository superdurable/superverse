// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { login, registerAccount } from "@/lib/api/account";
import { errorMessage } from "@/lib/api/transport/api-error";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { BrandMark } from "./BrandMark";

type Mode = "login" | "register";

const FIELD = cx(
  "mt-1.5 w-full rounded-md border border-line bg-well px-3 py-2 font-normal text-ink focus:border-accent focus:shadow-(--focus-ring) focus:outline-none",
);

const COPY = {
  login: {
    heading: "Welcome back",
    submit: "Sign in",
    failure: "Sign in failed",
    passwordAutoComplete: "current-password",
    switchPrompt: "New here? ",
    switchLabel: "Create an account",
    switchHref: studioRoutes.register,
  },
  register: {
    heading: "Create your account",
    submit: "Create account",
    failure: "Could not create the account",
    passwordAutoComplete: "new-password",
    switchPrompt: "Already registered? ",
    switchLabel: "Sign in",
    switchHref: studioRoutes.login,
  },
} as const;

/** Signs in or registers through /api/auth, which sets the login cookie, then opens Studio. */
export function AccountForm({ mode }: { mode: Mode }) {
  const router = useRouter();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const copy = COPY[mode];

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    const data = new FormData(event.currentTarget);
    const credentials = { email: String(data.get("email") ?? ""), password: String(data.get("password") ?? "") };
    try {
      await (mode === "login" ? login : registerAccount)(credentials);
      router.replace(studioRoutes.home);
      router.refresh();
    } catch (reason) {
      setError(errorMessage(reason, copy.failure));
    } finally {
      setBusy(false);
    }
  }

  return (
    // Scrolls rather than clipping when a short phone screen cannot fit the card.
    <main className="grid h-full overflow-y-auto bg-bg p-6 max-xs:p-4">
      <section className="m-auto w-full max-w-md rounded-xl border border-line-soft bg-panel p-8 shadow-(--shadow-lift) max-xs:p-6">
        <BrandMark />
        <h1 className="mt-6 text-2xl font-semibold">{copy.heading}</h1>
        <p className="mt-2 text-sm text-ink-dim">Build and manage your personal projects.</p>
        <form className="mt-6 flex flex-col gap-4" onSubmit={(event) => void submit(event)}>
          <label className="block text-sm font-medium text-ink-dim">
            Email
            <input name="email" aria-label="Email" type="email" autoComplete="email" required className={FIELD} />
          </label>
          <label className="block text-sm font-medium text-ink-dim">
            Password
            <input
              name="password"
              aria-label="Password"
              type="password"
              autoComplete={copy.passwordAutoComplete}
              className={FIELD}
            />
          </label>
          {error && <Banner role="alert">{error}</Banner>}
          <Button type="submit" variant="primary" disabled={busy} className="w-full">
            {busy ? "Please wait…" : copy.submit}
          </Button>
        </form>
        <p className="mt-5 text-sm text-ink-dim">
          {copy.switchPrompt}
          <Link className="text-accent hover:underline" href={copy.switchHref}>
            {copy.switchLabel}
          </Link>
        </p>
      </section>
    </main>
  );
}
