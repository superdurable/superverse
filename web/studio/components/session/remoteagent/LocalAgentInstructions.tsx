// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/Button";

/** One set of installation and foreground relay instructions for both pairing surfaces. */
export function LocalAgentInstructions({
  endpoint,
  showHelpLink = true,
  awaitingAcknowledgement = false,
}: {
  /** Omitted for public help; null keeps Project setup here while its endpoint loads. */
  endpoint?: string | null;
  showHelpLink?: boolean;
  awaitingAcknowledgement?: boolean;
}) {
  return (
    <div className="mt-4 space-y-3 text-sm text-ink-dim">
      <p>
        <strong className="text-ink">Before you connect:</strong> Use macOS with Homebrew. dexai checks Codex CLI or
        Claude Code first and offers to install it or sign in if needed. Claude Code requires version 2.1.41 or newer.
      </p>
      <ol className="list-decimal space-y-3 pl-5">
        <li>
          Install the command:
          <CopyCommand command="brew install superdurable/tap/dexai" />
        </li>
        <li>
          {endpoint === undefined ? (
            <>
              Open the Agent picker beside the message box and choose Connect a local agent. Copy the command shown
              there into Terminal.
            </>
          ) : endpoint ? (
            <>
              Run this in Terminal:
              <CopyCommand command={`dexai connect --endpoint ${endpoint}`} />
            </>
          ) : awaitingAcknowledgement ? (
            "Acknowledge the local-machine permissions above to show your pairing code and connection command here."
          ) : (
            "Studio will show your connection command here after the connection details load."
          )}
        </li>
        <li>
          Choose Codex or Claude Code and complete any setup prompts, then enter the pairing code shown in Studio when
          asked. Keep the code out of command arguments.
        </li>
      </ol>
      <p>
        The first brief gives your agent the Project context. Codex opens its TUI after that brief; Claude Code runs
        headless while you continue the conversation in Studio. Run dexai connect once per Project; several connections
        can run at once. Keep each terminal running.
      </p>
      <details className="rounded-lg border border-line-soft p-3">
        <summary className="cursor-pointer font-medium text-ink">Stop and reconnect</summary>
        <p className="mt-2">
          Exit the Codex TUI or press Ctrl+C in the Claude foreground terminal to stop and disconnect that connection.
          To reconnect, create a new pairing code in Studio and run the connect command again.
        </p>
        <p className="mt-2">Check connections or stop the only saved connection:</p>
        <CopyCommand command="dexai status" />
        <CopyCommand command="dexai disconnect" />
        <p className="mt-2">
          With several connections, use <code>dexai disconnect --connection ID</code>, replacing ID with the connection
          ID from <code>dexai status</code>. Other Projects keep running.
        </p>
      </details>
      {showHelpLink && (
        <Link
          href="/help/local-agent"
          target="_blank"
          rel="noreferrer"
          className="font-medium text-accent underline underline-offset-4"
        >
          Local agent help and troubleshooting (opens a new tab)
        </Link>
      )}
    </div>
  );
}

/** Clipboard failure leaves a selectable command and an accessible explanation. */
function CopyCommand({ command }: { command: string }) {
  const [notice, setNotice] = useState("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setNotice("Copied.");
    } catch {
      setNotice("Copy failed. Select and copy the command manually.");
    }
  }
  return (
    <div className="mt-2">
      <div className="flex items-start gap-2 rounded-md bg-well p-2">
        <code className="min-w-0 flex-1 overflow-x-auto p-1 text-xs wrap-anywhere whitespace-pre-wrap text-ink select-all">
          {command}
        </code>
        <Button size="sm" aria-label={`Copy ${command}`} onClick={() => void copy()}>
          Copy
        </Button>
      </div>
      {notice && (
        <p className="mt-1 text-xs" role="status">
          {notice}
        </p>
      )}
    </div>
  );
}
