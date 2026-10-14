// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, type ReactNode } from "react";
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";

const AccountContext = createContext<Readonly<AccountSnapshot__Output> | null | undefined>(undefined);

// The signed-in pages' server layout owns this read. Null is reserved for explicit mock
// development, where pages remain browsable without a mock login.
export function AccountProvider({
  account,
  children,
}: {
  account: Readonly<AccountSnapshot__Output> | null;
  children: ReactNode;
}) {
  return <AccountContext.Provider value={account}>{children}</AccountContext.Provider>;
}

export function useAccount() {
  const account = useContext(AccountContext);
  if (account === undefined) throw new Error("AccountProvider is missing");
  return account;
}
