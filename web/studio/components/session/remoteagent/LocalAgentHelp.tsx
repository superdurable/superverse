// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import Link from "next/link";
import { LocalAgentInstructions } from "./LocalAgentInstructions";

const QUESTIONS = [
  {
    question: "What happens after I enter the pairing code?",
    answer:
      "Choose Codex or Claude Code in Terminal. The connection receives a first brief with the Project context and instructions. Codex then opens its TUI; Claude Code runs headless and you use Studio to talk to it. Keep the relay terminal running. Project code stays in the Project Sandbox and the agent uses platform tools to work on it.",
  },
  {
    question: "The Codex daemon is not running. What should I do?",
    answer:
      "dexai starts an absent Codex app-server daemon automatically. If startup fails, its error includes Codex’s message. Check that Codex is installed and signed in, then try codex app-server daemon start yourself before connecting again. dexai does not update or restart a running daemon.",
  },
  {
    question: "Why do I see a version warning?",
    answer:
      "Versions below the supported minimum are refused. A newer version than the newest verified version starts with a compatibility warning; the warning also appears in dexai status. If the provider sends an unrecognized protocol response, the binding stops with an error. Check that error and update dexai with brew upgrade dexai before pairing again.",
  },
  {
    question: "My pairing code expired or was already used.",
    answer:
      "Codes are one-time and expire after ten minutes. In the connection dialog, create a new code. On the Project creation page, an expired code is renewed automatically while your first message stays there. Run the connect command again and enter the current code when asked.",
  },
  {
    question: "I am not logged in to Codex or Claude Code.",
    answer:
      "Open the chosen CLI on your Mac and finish its sign-in flow, then run dexai connect again with a current pairing code. Your local agent uses your own Codex or Claude account. Installing dexai does not sign you in to either provider.",
  },
  {
    question: "Why does Studio say my agent is offline?",
    answer:
      "Check the terminal for that Project and your network connection. Exiting the Codex TUI or stopping the Claude foreground command disconnects it; create a new pairing code and run dexai connect again. If the terminal is still open, read its error message and check your local agent login. Studio keeps messages queued while the agent is offline and does not switch agents automatically.",
  },
  {
    question: "What does ‘message may not have reached the agent’ mean?",
    answer:
      "The relay could not prove whether the local agent received that message. It does not automatically resend an uncertain delivery. Check Studio history and the local agent’s terminal or TUI to see whether it acted on the message. If it did not, send the message again from the composer. Sending again may duplicate a message that did arrive.",
  },
  {
    question: "How do I switch back to the Platform agent?",
    answer:
      "Open the Agent picker beside the message box and choose Use the Platform agent. This stops the connected agent’s current work and returns the Session to platform execution. You may need to set your model API key before sending. Switching back revokes the replaced connection; pairing again is required to use it later.",
  },
  {
    question: "How do I stop or reconnect?",
    answer:
      "Run dexai connect once for each Project you want to use; several connections can run independently. Exit the Codex TUI or press Ctrl+C in the Claude foreground terminal to stop and disconnect that connection. To reconnect, create a new pairing code in Studio and run dexai connect again. dexai status lists connections; with several, use dexai disconnect --connection ID with the ID from status to stop just one. Keep each terminal running while you work in that Project.",
  },
  {
    question: "Where does dexai keep local state?",
    answer:
      "On macOS, private state is under ~/Library/Application Support/dexai by default. Each connect gets its own directory under connections/. DEXAI_STATE_DIR or --state-dir changes the root; separate overrides are not needed for concurrent Projects. Normal exit revokes the connection and removes its directory. Failed cleanup leaves a record for dexai status and targeted dexai disconnect. The state includes credentials; do not share it. Codex and Claude retain their own conversation history separately.",
  },
  {
    question: "What can Claude Code access?",
    answer:
      "dexai runs Claude Code headless in a sandbox, with dontAsk permission mode and only the platform MCP tools allowed. It does not grant general local shell or filesystem tools. Project work happens through those platform tools in the Project Sandbox. Project messages and application failure text still reach your local agent.",
  },
  {
    question: "Which directory does Codex use?",
    answer:
      "The default is a private scratch directory inside that connection’s state directory, removed on normal exit. dexai connect --cwd <directory> selects an existing local directory instead. This local directory is for the Codex process and does not mirror Project source. Codex’s local permissions come from your Codex configuration; the Project source stays in the platform Sandbox.",
  },
  {
    question: "How do I update dexai?",
    answer:
      "Run brew upgrade dexai. Stop the foreground connections before updating, then reconnect each Project with a new pairing code. Your Codex CLI or Claude Code installation is managed separately.",
  },
];

/** Publicly readable setup guidance does not require a Project or an active pairing code. */
export function LocalAgentHelp() {
  return (
    <main className="h-full overflow-y-auto bg-panel text-ink">
      <div className="mx-auto max-w-3xl px-5 py-8 sm:px-8">
        <Link href="/" className="text-sm text-ink-dim underline underline-offset-4">
          Back to Studio
        </Link>
        <h1 className="mt-5 text-2xl font-semibold">Connect your local agent</h1>
        <p className="mt-3 text-reading text-ink-dim">
          Use your own Codex or Claude Code from your Mac while collaborating in Studio.
        </p>
        <section aria-labelledby="local-agent-setup" className="mt-7">
          <h2 id="local-agent-setup" className="text-lg font-semibold">
            Install and connect
          </h2>
          <LocalAgentInstructions showHelpLink={false} />
        </section>
        <section aria-labelledby="local-agent-questions" className="mt-8">
          <h2 id="local-agent-questions" className="text-lg font-semibold">
            Questions and troubleshooting
          </h2>
          <div className="mt-4 space-y-3">
            {QUESTIONS.map(({ question, answer }) => (
              <details key={question} className="rounded-lg border border-line-soft p-4">
                <summary className="cursor-pointer font-medium">{question}</summary>
                <p className="mt-3 text-sm leading-relaxed text-ink-dim">{answer}</p>
              </details>
            ))}
          </div>
        </section>
      </div>
    </main>
  );
}
