// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock's examples: the examples Studio bundles (lib/examples), each with the card the mock Session
// records as version 1 when a Project opens it and the Git tree its generated design names.
import { EXAMPLE_CATALOG } from "@/lib/examples/example-catalog";
import type { AgentRequirements__Output } from "@/lib/generated/protobuf/superverse/v2/AgentRequirements";
import { EXAMPLE_DESIGNS } from "@/lib/generated/template-design/example-designs";

interface MockExample {
  exampleId: string;
  title: string;
  /** The tree of the example's generated design, so that a Project opened from it draws the example's Flows. */
  tree: string;
  /** The example's requirements card, without a design: the mock draws the example's own Flows instead. */
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

/** The card of every bundled example, by example ID; the catalog check below keeps the two alike. */
const MOCK_CARDS: Readonly<Record<string, AgentRequirements__Output>> = {
  "text-summary": card(
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
  "github-releases-newsletter": card(
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
  "slack-thread-approval": card(
    "Slack thread approval",
    "A process that follows threads in one Slack channel: a new message starts it, and a reply in its thread finishes it with a confirmation posted in the thread.",
    [
      "A new top-level message in the configured Slack channel starts one approval for its thread; a redelivered message starts no second one.",
      "The first matching reply in that thread is accepted and moves the approval on; a later reply changes nothing.",
      "After the reply, the application posts Processing complete. into the same thread and completes the approval.",
    ],
  ),
  "summary-approval": card(
    "Summary approval",
    "A person approves each generated summary in Studio before it is saved.",
    [
      "A Step AwaitSummaryApproval holds the generated summary and shows awaiting-approval until a person decides.",
      "An Approve summary Action releases that Step, which then saves the summary.",
    ],
  ),
};

/** The examples the mock offers: every example Studio bundles, in catalog order, each with its card and tree. */
export const MOCK_EXAMPLES: readonly MockExample[] = EXAMPLE_CATALOG.map(({ exampleId, title }) => {
  const mockCard = MOCK_CARDS[exampleId];
  const design = EXAMPLE_DESIGNS[exampleId];
  if (!mockCard || !design) throw new Error(`The mock has no card or design of the bundled example ${exampleId}`);
  return { exampleId, title, tree: design.tree, card: mockCard };
});
for (const exampleId of Object.keys(MOCK_CARDS))
  if (!EXAMPLE_CATALOG.some((example) => example.exampleId === exampleId))
    throw new Error(`The mock has a card of ${exampleId}, which Studio does not bundle`);
