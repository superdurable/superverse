// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { BrandMark } from "@/components/shell/BrandMark";
import Link from "next/link";

export default function RestartLoginPage() {
  return (
    <main className="grid h-full bg-bg p-6">
      <section className="m-auto w-full max-w-md rounded-xl border border-line-soft bg-panel p-8 shadow-(--shadow-lift)">
        <BrandMark />
        <h1 className="mt-6 text-2xl font-semibold">Sign-in link expired</h1>
        <p className="mt-2 text-sm text-ink-dim">Start again to continue to Dex AI Platform.</p>
        <Link
          className="mt-6 block rounded-md bg-accent px-4 py-2 text-center font-medium text-white"
          href="/api/auth/start"
        >
          Restart sign-in
        </Link>
      </section>
    </main>
  );
}
