// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock's examples: the server's embedded examples as its catalog lists them, each with the card the
// mock Session records as version 1 when a Project opens it.
import type { AgentRequirements__Output } from "@/lib/generated/protobuf/superverse/v2/AgentRequirements";
import type { Example } from "@/lib/generated/protobuf/superverse/v2/Example";

/** The Git tree the mock records for every example, illustrative only. */
export const MOCK_EXAMPLE_TREE = "59acb34868ff760cba38688a7455f8888000fe49";

interface MockExample extends Example {
  exampleId: string;
  title: string;
  /** The example's requirements card, without a design: the mock draws its fixture processes instead. */
  card: AgentRequirements__Output;
}

function card(title: string, summary: string, features: readonly string[]): AgentRequirements__Output {
  return {
    title,
    summary,
    features: features.map((text, index) => ({ id: `R${String(index + 1)}`, text })),
    connections: [],
    userSetup: [],
    acceptance: [],
    design: null,
    designBuilt: false,
    designBuiltCommit: "",
    designWrittenSteps: [],
  };
}

export const MOCK_EXAMPLES: readonly MockExample[] = [
  {
    exampleId: "text-summary",
    title: "Text summary",
    category: "EXAMPLE_CATEGORY_CONTENT",
    apps: ["An AI model"],
    decider: "No one: the summary is written on its own",
    description:
      "When someone pastes a text into the page, the configured AI model writes a short factual summary of it. The page shows the summary with the model that wrote it and the tokens it used, and marks one the model cut short. A blank or too long text is refused before any paid request, and a refused or failed answer is shown with what went wrong instead of a summary.",
    connectorIds: ["llm"],
    card: card(
      "Text summary",
      "A page where a person submits a text and gets a short factual summary from the configured AI model, with the model and token usage shown.",
      [
        "A blank, too long or invalid text is refused before any model request, and the refused submission starts no summary.",
        "A repeated submission with the same request ID starts one summary.",
        "The configured model writes a short factual summary, which the page shows with the model and token usage; a summary cut at the output limit is shown as truncated.",
        "A blocked, rejected, invalid or failed model answer shows its status and a notice and no summary.",
        "A lost model response is recorded as unknown without sending the paid request again.",
      ],
    ),
  },
  {
    exampleId: "github-releases-newsletter",
    title: "GitHub releases newsletter",
    category: "EXAMPLE_CATEGORY_CONTENT",
    apps: ["Slack", "GitHub", "OpenAI", "Gmail"],
    decider: "An administrator posting in Slack",
    description:
      "Visitors subscribe once with their email address on the page. When an administrator posts publish newsletter in the Slack channel, the process reads the last seven days of releases of two GitHub repositories, has OpenAI write one factual report, and sends it to every subscriber as their own Gmail message. A failed read or model answer sends nothing, and an uncertain send is never repeated.",
    connectorIds: ["slack", "github", "llm", "gmail"],
    card: card(
      "GitHub Releases Newsletter",
      "Visitors subscribe once; an authorized Slack message starts a release newsletter and each subscriber receives an individual Gmail delivery.",
      [
        "The public page accepts valid email subscriptions, retains each normalized address once and gives clear feedback.",
        "Only a selected administrator posting the exact top-level Slack message publish newsletter in the selected channel starts a campaign; duplicate delivery of that message starts none.",
        "The campaign reads every release page from superdurable/dex and dex-connectors-library, excludes drafts, filters by the previous seven days and keeps original links.",
        "OpenAI writes a factual newsletter from release notes, or the report clearly says there are no releases; source or model failure never sends an incomplete report.",
        "The campaign freezes subscribers at trigger time; Gmail sends the same report to each individually and records sent, failed or uncertain delivery without automatically retrying an uncertain send.",
      ],
    ),
  },
  {
    exampleId: "slack-thread-approval",
    title: "Slack thread approval",
    category: "EXAMPLE_CATEGORY_APPROVALS",
    apps: ["Slack"],
    decider: "The members allowed to reply in the thread",
    description:
      "When someone posts a new message in the approval channel in Slack, a process starts for that thread and waits. The first reply in the thread from one of the members allowed to approve finishes it: the process posts “Processing complete.” in the thread. Later replies change nothing, and a message Slack delivers twice starts only one process.",
    connectorIds: ["slack"],
    card: card(
      "Slack thread approval",
      "A process that follows threads in one Slack channel: a new message starts it, and a reply in its thread finishes it with a confirmation posted in the thread.",
      [
        "A new top-level message in the configured Slack channel starts one approval for its thread; a redelivered message starts no second one.",
        "The first matching reply in that thread is accepted and moves the approval on; a later reply changes nothing.",
        "After the reply, the application posts Processing complete. into the same thread and completes the approval.",
      ],
    ),
  },
  {
    exampleId: "summary-approval",
    title: "Summary approval",
    category: "EXAMPLE_CATEGORY_APPROVALS",
    apps: ["An AI model"],
    decider: "A person in Studio",
    description:
      "When someone pastes a text into the page, the configured AI model writes a short summary of it, and the summary waits for a person to approve it in Studio before it is saved. A truncated summary is saved without waiting, and a blocked, refused or failed answer is recorded with what went wrong instead of a summary.",
    connectorIds: ["llm"],
    card: card("Summary approval", "A person approves each generated summary in Studio before it is saved.", [
      "A Step AwaitSummaryApproval holds the generated summary and shows awaiting-approval until a person decides.",
      "An Approve summary Action releases that Step, which then saves the summary.",
    ]),
  },
];
