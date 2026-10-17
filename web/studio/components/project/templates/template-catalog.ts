// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ExampleCategory, ExampleView } from "@/lib/api/example";

/** The groups the start page's templates are filed under. */
type TemplateCategory = ExampleCategory;

/** What every starting point on the start page shows. */
interface TemplateFields {
  id: string;
  title: string;
  category: TemplateCategory;
  /** The apps the process works with, as their makers name them. */
  apps: readonly string[];
  /** Who makes the call the process waits for. */
  decider: string;
  /** A full description: what starts it, the people, the rules and the exceptions. */
  description: string;
}

/**
 * A placeholder is only a description: picking it fills the composer, and the agent builds the
 * process from that text like any other.
 */
export interface PlaceholderTemplate extends TemplateFields {
  kind: "placeholder";
}

/**
 * A built example of the server's application template: Open example creates the Project from its
 * finished, tested source as it is, with its requirements recorded as the first version, and what the
 * person typed waits in the Project's composer as an unsent draft.
 */
export interface BuiltExample extends TemplateFields {
  kind: "example";
}

/** A starting point on the start page. */
export type ProcessTemplate = PlaceholderTemplate | BuiltExample;

/** The start page's starting point of a built example. */
export function builtExample(example: ExampleView): BuiltExample {
  const { exampleId, title, category, apps, decider, description } = example;
  return { id: exampleId, title, category, apps, decider, description, kind: "example" };
}

/** The templates in the order the start page shows them. */
export const PROCESS_TEMPLATES: readonly PlaceholderTemplate[] = [
  {
    id: "refund-approvals",
    title: "Refund approvals",
    category: "Approvals",
    apps: ["Gmail", "Slack", "Stripe"],
    decider: "A support lead",
    description:
      "When a customer emails asking for a refund, find their order and the amount from the email. Refunds under $50 are approved on their own. Anything larger goes to a support lead in Slack with the order details, and they approve or decline it. An approved refund is paid back in Stripe and the customer gets a reply saying so. If the order cannot be found or Stripe refuses the refund, tell the support lead in Slack and pay nothing until they decide.",
    kind: "placeholder",
  },
  {
    id: "expense-approvals",
    title: "Expense approvals",
    category: "Finance",
    apps: ["Slack", "Google Sheets"],
    decider: "The employee's manager",
    description:
      "When an employee submits an expense in Slack with a photo of the receipt, check that it has an amount, a date and a category. Expenses under $100 are approved on their own. Larger ones go to the employee's manager, and anything over $1,000 also needs someone in finance. Approved expenses are added to the reimbursement sheet. If the receipt is missing or unreadable, ask the employee to send it again before anyone reviews it.",
    kind: "placeholder",
  },
  {
    id: "invoice-approval",
    title: "Invoice approval",
    category: "Finance",
    apps: ["Gmail", "Slack", "QuickBooks"],
    decider: "A finance manager",
    description:
      "When a supplier emails an invoice, read the supplier, the amount and the due date. An invoice from a known supplier that matches an open purchase order is approved on its own. Every other invoice goes to a finance manager in Slack, who approves it, rejects it or asks the supplier a question. Approved invoices are recorded in QuickBooks to be paid before the due date. If an invoice looks like one already received, hold it and tell the finance manager.",
    kind: "placeholder",
  },
  {
    id: "weekly-ticket-digest",
    title: "Weekly ticket digest",
    category: "Support",
    apps: ["Zendesk", "Slack"],
    decider: "The support lead",
    description:
      "Every Monday at 9am, gather last week's support tickets: how many came in, how many were solved, the slowest replies and the most common topics. Write a short summary and send it to the support lead in Slack to check. Once they approve it, post it in the support team's channel. If the ticket system cannot be reached, tell the support lead and try again an hour later.",
    kind: "placeholder",
  },
  {
    id: "time-off-requests",
    title: "Time off requests",
    category: "People",
    apps: ["Slack", "Google Calendar"],
    decider: "The person's manager",
    description:
      "When someone asks for time off in Slack, record the dates and the reason. Their manager approves or declines it in Slack. Approved time off goes on the team calendar and the person gets a confirmation. Requests for more than ten working days also need someone in HR. If the manager has not answered within two working days, remind them, and after four tell HR.",
    kind: "placeholder",
  },
  {
    id: "new-hire-accounts",
    title: "New hire accounts",
    category: "People",
    apps: ["Gmail", "Slack", "Google Workspace"],
    decider: "The hiring manager",
    description:
      "When HR emails that a new hire signed their offer, ask the hiring manager in Slack which team and tools the person needs. Once they answer, create the person's work account in Google Workspace, add them to the team's Slack channels and email them their first-day details. If an account with that name already exists, stop and ask the hiring manager before creating anything.",
    kind: "placeholder",
  },
];
