// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { AccountForm } from "@/components/shell/AccountForm";
import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { authConfig, originHost } from "@/lib/server/auth-config";
import { LoginHostEntry } from "./LoginHostEntry";

export default async function LoginPage() {
  const config = authConfig();
  if (config.handoffEnabled || config.exchangeEnabled) {
    const host = (await headers()).get("host");
    if (host === originHost(config.productOrigin)) redirect("/api/auth/start");
    if (host === originHost(config.loginOrigin)) return <LoginHostEntry mode="login" />;
  }
  return <AccountForm mode="login" />;
}
