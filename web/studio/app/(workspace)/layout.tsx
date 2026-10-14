// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ReactNode } from "react";
import { headers } from "next/headers";
import { redirect } from "next/navigation";
import { AppShell } from "@/components/shell/AppShell";
import { AccountProvider } from "@/lib/account/AccountProvider";
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";
import { callerFromCookieHeader } from "@/lib/server/caller-envelope";
import { grpcBackend } from "@/lib/server/grpc-client";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { TeamSpaceProvider } from "@/lib/teamspace/TeamSpaceProvider";
import { mockBackend } from "@/mock/backend/mock-backend";
import { isStudioMockEnabled } from "@/mock/enabled";

// The workspace reads the signed-in account once per document, in mock mode too; every page below it works in
// one TeamSpace at a time.
export default async function WorkspaceLayout({ children }: { children: ReactNode }) {
  const requestHeaders = await headers();
  const caller = callerFromCookieHeader(requestHeaders.get("cookie"));
  if (!caller) redirect(studioRoutes.login);
  // GetCurrentAccount never opens the mock store, so this guessed origin never becomes its origin.
  const backend = isStudioMockEnabled() ? mockBackend(`http://${requestHeaders.get("host")}`) : grpcBackend;
  let account: AccountSnapshot__Output | null;
  try {
    ({ account } = await backend.unary("GetCurrentAccount", {}, caller));
  } catch (error) {
    const { status } = error as { status?: number };
    if (status === 401 || status === 403) redirect(studioRoutes.login);
    throw error;
  }
  return (
    <AccountProvider account={account}>
      <TeamSpaceProvider>
        <AppShell>{children}</AppShell>
      </TeamSpaceProvider>
    </AccountProvider>
  );
}
