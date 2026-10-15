// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readPrivateJSON } from "@/tests/e2e/support/private-files";

export type OAuth = { clientId: string; clientSecret: string };
export type Credentials = {
  slack: OAuth & { appToken: string; adminUserToken?: string; teamId: string; channelId: string; adminUserId: string };
  github: OAuth;
  gmail: OAuth;
  openai: { apiKey: string; model: string };
  recipientEmail: string;
};

export function credentials(reuse: boolean): Credentials {
  const path = process.env.SUPERVERSE_E2E_NEWSLETTER_FILE;
  if (!path) throw new Error("SUPERVERSE_E2E_NEWSLETTER_FILE must name a private JSON file");
  const value = readPrivateJSON(path, "Newsletter provider credentials", 16 * 1024) as Credentials;
  if (
    !value?.slack?.teamId ||
    !value.slack.channelId ||
    !value.slack.adminUserId ||
    (reuse && !value.slack.adminUserToken) ||
    (!reuse &&
      (!value.slack.clientId ||
        !value.slack.clientSecret ||
        !value.slack.appToken ||
        !value?.github?.clientId ||
        !value.github.clientSecret ||
        !value?.gmail?.clientId ||
        !value.gmail.clientSecret ||
        !value?.openai?.apiKey ||
        !value.openai.model)) ||
    !value.recipientEmail?.includes("@")
  )
    throw new Error("Newsletter provider credentials are incomplete");
  return value;
}

export function administratorCredentials(): { email: string; password: string } {
  const path = process.env.SUPERVERSE_E2E_ADMINISTRATOR_FILE;
  if (!path) throw new Error("SUPERVERSE_E2E_ADMINISTRATOR_FILE must name private platform-admin credentials");
  const value = readPrivateJSON(path, "Private administrator credentials", 16 * 1024) as {
    email?: string;
    password?: string;
  };
  if (!value?.email || !value.password) throw new Error("Invalid private platform-admin credentials");
  return { email: value.email, password: value.password };
}
