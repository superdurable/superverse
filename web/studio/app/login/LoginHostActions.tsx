// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { authorizeLoginHost, logoutLoginHost } from "@/lib/api/account";

export function LoginHostActions({
  email,
  csrf,
  logoutCSRF,
  productOrigin,
}: {
  email: string;
  csrf?: string;
  logoutCSRF: string;
  productOrigin: string;
}) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function continueToStudio() {
    if (!csrf) return;
    setBusy(true);
    setError("");
    try {
      await authorizeLoginHost(csrf);
      window.location.assign(new URL("/api/auth/continue", window.location.origin).toString());
    } catch {
      setError("Could not continue. Please start sign in again.");
      setBusy(false);
    }
  }
  async function switchAccount() {
    setBusy(true);
    setError("");
    try {
      await logoutLoginHost(logoutCSRF);
      router.refresh();
    } catch {
      setError("Could not switch accounts. Please try again.");
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="mt-6 space-y-4">
      <p className="text-sm text-ink-dim">Signed in as {email}</p>
      {csrf ? (
        <button
          className="block rounded-md bg-accent px-4 py-2 font-medium text-white"
          onClick={() => void continueToStudio()}
          disabled={busy}
        >
          Continue to Dex AI Platform
        </button>
      ) : (
        <a
          className="block rounded-md bg-accent px-4 py-2 text-center font-medium text-white"
          href={`${productOrigin}/api/auth/start`}
        >
          Dex AI Platform
        </a>
      )}
      <button
        className="block text-sm text-accent hover:underline"
        onClick={() => void switchAccount()}
        disabled={busy}
      >
        Use another account
      </button>
      {error && (
        <p className="text-sm text-red-600" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
