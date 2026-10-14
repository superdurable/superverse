// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock AccountService: in-memory accounts and logins, reached only through /api/auth.
import { mockAccount, mockLogout, mockSignIn } from "@/mock/store/accounts";
import type { MockService } from "./mock-backend";

export const accountService: MockService<"AccountService"> = {
  RegisterAccount: ({ email, password }) => ({ login: mockSignIn("register", { email, password }) }),
  Login: ({ email, password }) => ({ login: mockSignIn("login", { email, password }) }),
  Logout: ({ caller }) => {
    mockLogout(caller?.sessionToken ?? "");
    return {};
  },
  GetCurrentAccount: ({ caller }) => ({ account: mockAccount(caller) }),
};
