// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { newRequestId } from "@/lib/api/transport/api-error";
import { hasPendingProjectCreation, storePendingProjectCreation } from "@/lib/project/project-intents";
import type { AgentModel } from "@/lib/session/agent-types";
import { useMockScenarios, type MockInputChoice } from "@/lib/session/mock-scenarios";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Composer } from "@/components/session/composer/Composer";
import { MockComposer } from "@/components/session/mock/MockComposer";
import { SessionModelPicker } from "@/components/session/SessionModelPicker";
import { useStartingModel } from "@/components/session/start/use-starting-model";
import { Banner } from "@/components/ui/Banner";
import { cx } from "@/components/ui/cx";

const EXAMPLES = [
  "Build a customer-support inbox with search, tags, and a clean responsive UI.",
  "Create a habit tracker with streaks, weekly charts, and a dark mode.",
  "Make a lightweight CRM where I can import contacts and track follow-ups.",
];

/** The first message is the first input of a scenario, which answers no pending input. */
function noPendingInput(): Promise<never> {
  throw new Error("The first message answers no pending input");
}

const READING_MODELS = "Reading the model options…";

/**
 * The creation page: the Project's name, the model of its Session and its first message. The page
 * shows the Session's own model picker and composer, preselected on the model a new Session starts
 * on: Sonnet, or the mock model for the platform administrator where the server offers it. On the
 * mock model the composer takes the first input of a scenario in place of free text. Nothing is
 * sent here: the choice is stored with the creation, and Build applies the model and sends the
 * message once the Session starts.
 */
export function ProjectStartForm({ teamSpaceId }: { teamSpaceId: string }) {
  const router = useRouter();
  const mockScenarios = useMockScenarios();
  const [name, setName] = useState("My app");
  const [chosen, setChosen] = useState<AgentModel | null>(null);
  const startingModel = useStartingModel(mockScenarios);
  // Null only while the administrator's offer of the mock model is being read.
  const model = chosen ?? startingModel;
  const [formError, setFormError] = useState<string | null>(null);
  const [transitioning, setTransitioning] = useState(false);

  useEffect(() => {
    if (hasPendingProjectCreation()) router.replace(studioRoutes.projectCreation);
  }, [router]);

  // Rejects when the creation cannot start, so that the composer keeps what the person wrote.
  async function beginCreation(prompt: string, mock: MockInputChoice | null = null) {
    const projectName = name.trim();
    if (model === null) throw new Error(READING_MODELS);
    if (!projectName) {
      setFormError("Give the project a name first");
      throw new Error("The project has no name");
    }
    try {
      storePendingProjectCreation({
        teamSpaceId,
        name: projectName,
        prompt,
        model,
        mock,
        idempotencyKey: newRequestId(),
        messageIdempotencyKey: newRequestId(),
      });
    } catch (reason) {
      setFormError(reason instanceof Error ? reason.message : "Could not keep the project request in this browser");
      throw reason;
    }
    setTransitioning(true);
    router.push(studioRoutes.projectCreation);
  }

  return (
    <main className="flex min-h-0 min-w-0 flex-1 items-center-safe justify-center-safe overflow-y-auto p-8 max-sm:p-4">
      <div className="w-full max-w-2xl py-8">
        <h1 className="text-center text-3xl font-semibold tracking-tight">Create a project.</h1>
        <p className="mx-auto mt-3 max-w-xl text-center text-base leading-relaxed text-ink-dim">
          Start from the trusted template in a new private repository.
        </p>
        <div className="mt-6">
          <label className="mb-2 block text-sm font-medium text-ink-dim" htmlFor="project-name">
            Project name
          </label>
          <input
            id="project-name"
            value={name}
            maxLength={200}
            disabled={transitioning}
            onChange={(event) => setName(event.target.value)}
            className={cx(
              "w-full rounded-lg border border-line bg-panel px-3 py-2 text-base text-ink placeholder:text-ink-faint focus:border-accent focus:shadow-(--focus-ring) focus:outline-none disabled:opacity-50",
            )}
            placeholder="My app"
          />
        </div>
        {formError && <Banner className="mt-4">{formError}</Banner>}
        <SessionModelPicker
          className="mt-6 rounded-lg border border-line-soft"
          model={model}
          mockSelectable={mockScenarios?.selectable ?? false}
          disabled={transitioning || model === null}
          onSelect={setChosen}
        />
        <div className="mt-6">
          {model === "mock" ? (
            <MockComposer
              catalog={mockScenarios}
              progress={null}
              turnRunning={false}
              lastSequence={0}
              pendingInput={null}
              busy={transitioning}
              disabledReason={null}
              newProject
              sendLabel="Build"
              className="rounded-xl"
              onSend={beginCreation}
              onDecide={noPendingInput}
              onAnswer={noPendingInput}
            />
          ) : (
            <Composer
              large
              autoFocus
              busy={transitioning}
              disabledReason={model === null ? READING_MODELS : null}
              placeholder="Describe the app you want to build…"
              onSend={beginCreation}
            />
          )}
        </div>
        {model !== "mock" && (
          <div className="mt-6 flex flex-wrap justify-center gap-2">
            {EXAMPLES.map((example) => (
              <button
                key={example}
                type="button"
                disabled={transitioning || model === null}
                onClick={() => void beginCreation(example).catch(() => undefined)}
                className="rounded-full border border-line-soft px-3 py-1.5 text-xs text-ink-faint enabled:hover:border-line enabled:hover:text-ink-dim disabled:opacity-40"
              >
                {example}
              </button>
            ))}
          </div>
        )}
      </div>
    </main>
  );
}
