// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Metadata } from "next";
import { connection } from "next/server";
import { headers } from "next/headers";
import { STORAGE_KEYS } from "@/lib/browser/storage";
import "./globals.css";

export const metadata: Metadata = {
  title: "Dex AI Platform",
  description: "Discuss, design, and build an application with a durable session agent.",
};

// Paints the stored theme before hydration, so a dark-mode page never flashes
// light. ThemeToggle follows later changes with the same resolution rule.
const THEME_BOOTSTRAP = `(function(){try{var p=localStorage.getItem(${JSON.stringify(STORAGE_KEYS.theme)});if(p!=='light'&&p!=='dark'&&p!=='auto')p='auto';var dark=window.matchMedia('(prefers-color-scheme: dark)').matches;var t=p==='auto'?(dark?'dark':'light'):p;var r=document.documentElement;r.dataset.theme=t;r.style.colorScheme=t;}catch(e){document.documentElement.dataset.theme='light';document.documentElement.style.colorScheme='light';}})();`;

export default async function RootLayout({ children }: LayoutProps<"/">) {
  await connection();
  const nonce = (await headers()).get("x-nonce") ?? undefined;

  return (
    <html lang="en" className="h-full antialiased" suppressHydrationWarning>
      <head>
        <script nonce={nonce} dangerouslySetInnerHTML={{ __html: THEME_BOOTSTRAP }} />
      </head>
      <body className="h-full overflow-hidden">
        <div className="h-full min-h-0">{children}</div>
      </body>
    </html>
  );
}
