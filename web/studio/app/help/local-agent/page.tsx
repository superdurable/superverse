// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Metadata } from "next";
import { LocalAgentHelp } from "@/components/session/remoteagent/LocalAgentHelp";

export const metadata: Metadata = {
  title: "Local agent help | Dex AI",
  description: "Install dexai, connect Codex or Claude Code, and troubleshoot your local agent connection.",
};

export default function LocalAgentHelpPage() {
  return <LocalAgentHelp />;
}
