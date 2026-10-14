// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The questions and the requirements card the mock Agent asks; development only.
import type { AgentFlowDesign__Output } from "@/lib/generated/protobuf/superverse/v2/AgentFlowDesign";
import type { AgentFlowDesignStep__Output } from "@/lib/generated/protobuf/superverse/v2/AgentFlowDesignStep";
import type { AgentFlowDesignTransition__Output } from "@/lib/generated/protobuf/superverse/v2/AgentFlowDesignTransition";
import type { AgentRequirements__Output } from "@/lib/generated/protobuf/superverse/v2/AgentRequirements";
import type { AgentUserInputQuestion__Output } from "@/lib/generated/protobuf/superverse/v2/AgentUserInputQuestion";

/** The three questions of the `ask questions` script, with long headings and options. */
export const MOCK_QUESTIONS: AgentUserInputQuestion__Output[] = [
  {
    id: "q1",
    header: "Which frontend presentation should this project use?",
    question: "Choose the visual direction for the application shell and its primary navigation.",
    options: [
      { label: "Split pane", description: "List + detail" },
      { label: "Single column", description: "Compact mobile" },
    ],
  },
  {
    id: "q2",
    header: "What change?",
    question: "What change do you want? Choose one or select Other and enter the exact text or code after choosing it.",
    options: [
      { label: "Update header/lede", description: "Replace the H1 or lede paragraph text" },
      { label: "Add an input field", description: "Add another input to the form" },
      { label: "Custom change", description: "Enter the exact text or code to apply" },
    ],
  },
  {
    id: "q3",
    header: "Commit & checks?",
    question: "Should the change be committed after the complete verification suite passes?",
    options: [
      { label: "Commit after checks", description: "Verify first, then create one commit" },
      { label: "Leave uncommitted", description: "Keep the verified change in the worktree" },
    ],
  },
];

/** The requirements card of the `confirm requirements` script: two features and two connections. */
export const MOCK_REQUIREMENTS: AgentRequirements__Output = {
  title: "Release newsletter",
  summary: "A weekly newsletter of the latest releases that an administrator reviews before it is sent.",
  features: [
    { id: "R1", text: "Visitors subscribe with their email address, and each address is stored once." },
    { id: "R2", text: "An administrator reviews the draft and approves it or asks for a revision." },
  ],
  connections: [
    {
      need: "Read the releases of the last seven days",
      connector: "github",
      operations: ["listReleases"],
      status: "STATUS_EXISTING",
      userSetup: "",
    },
    {
      need: "Send the approved newsletter by email",
      connector: "gmail",
      operations: ["sendEmail"],
      status: "STATUS_NEW",
      userSetup: "Connect the Gmail account that sends the newsletter.",
    },
  ],
  userSetup: ["Connect the Gmail account in Studio."],
  acceptance: ["A test newsletter arrives in the administrator's mailbox after approval."],
  design: {
    digest: "sha256:a6322ddfe9fd0f5bdb374310e202e70c1557fde6d89f872a999762b26b764beb",
    summary:
      "Each week a draft collects the latest releases and waits until an administrator approves it; then it is sent.",
    outline:
      "ReleaseNewsletterFlow: CollectReleases, DraftNewsletter, AwaitReview [Approve newsletter], SendNewsletter; +1 failure-handling Step",
  },
  designBuilt: false,
  designBuiltCommit: "",
};

/** The body of MOCK_REQUIREMENTS' Flow design, which GetFlowDesign returns at its digest. */
export const MOCK_FLOW_DESIGN: AgentFlowDesign__Output = {
  digest: MOCK_REQUIREMENTS.design?.digest ?? "",
  summary: MOCK_REQUIREMENTS.design?.summary ?? "",
  removedFlows: [],
  flows: [
    {
      flowType: "ReleaseNewsletterFlow",
      purpose: "Collects the week's releases into a draft that an administrator approves before it is sent.",
      startStep: "CollectReleases",
      steps: [
        mockStep("CollectReleases", "Releases", "Read the releases of the last seven days.", [["DraftNewsletter", ""]]),
        mockStep("DraftNewsletter", "Draft", "Write the draft from the collected releases.", [
          ["AwaitReview", "drafted"],
          ["RecordDraftFailure", "the model response was lost", true],
        ]),
        {
          ...mockStep("AwaitReview", "Review", "Wait for an administrator to approve the draft.", [
            ["SendNewsletter", ""],
          ]),
          waitsFor: { kind: "KIND_ACTION", name: "ApproveNewsletter" },
          features: ["R2"],
        },
        mockStep("SendNewsletter", "Send", "Send the approved newsletter by email.", [["", "", false, "END_COMPLETE"]]),
        mockStep("RecordDraftFailure", "Draft", "Record a lost draft without sending the paid request again.", [
          ["", "", false, "END_FAIL"],
        ]),
      ],
      channels: [{ name: "NewsletterApprovals", payload: "the approval" }],
      actions: [
        {
          label: "Approve newsletter",
          rpc: "ApproveNewsletter",
          publishes: "NewsletterApprovals",
          permission: "newsletter.approve",
        },
      ],
      attributes: [],
      starts: [],
    },
  ],
};

type MockNext = [
  toStep: string,
  when: string,
  isFailureRoute?: boolean,
  end?: AgentFlowDesignTransition__Output["end"],
];

function mockStep(stepType: string, group: string, explanation: string, next: MockNext[]): AgentFlowDesignStep__Output {
  return {
    stepType,
    group,
    explanation,
    waitsFor: { kind: "KIND_NONE", name: "" },
    next: next.map(([toStep, when, isFailureRoute = false, end = "END_UNSPECIFIED"]) => ({
      toStep,
      when,
      isFailureRoute,
      end,
    })),
    features: [],
  };
}
