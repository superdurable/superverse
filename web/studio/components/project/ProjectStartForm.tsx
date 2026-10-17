// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { newRequestId } from "@/lib/api/transport/api-error";
import {
  hasPendingProjectCreation,
  readProjectIntent,
  PROJECT_EXECUTOR,
  storePendingProjectCreation,
  type ProjectExecutor,
} from "@/lib/project/project-intents";
import type { AgentModel } from "@/lib/session/agent-types";
import { useMockScenarios } from "@/lib/session/mock-scenarios";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Composer } from "@/components/session/composer/Composer";
import { MockComposer } from "@/components/session/mock/MockComposer";
import { StartingModelPicker } from "@/components/session/start/StartingModelPicker";
import { useStartingModel } from "@/components/session/start/use-starting-model";
import { Banner } from "@/components/ui/Banner";
import { cx } from "@/components/ui/cx";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { ExampleGrid } from "./templates/ExampleGrid";
import { projectNameFrom } from "./templates/project-name";
import type { BuiltExample, ProcessTemplate } from "./templates/template-catalog";
import { useExamples } from "./templates/use-examples";

/** The name of a Project whose description gives none, such as one of punctuation only. */
const FALLBACK_NAME = "New process";

/** The first message is the first input of a scenario, which answers no pending input. */
function noPendingInput(): Promise<never> {
  throw new Error("The first message answers no pending input");
}

const READING_MODELS = "Reading the model options…";

/**
 * The start page: the description of the process, which Build sends as the first message of the Project's
 * Session, and under it the grid of starting points, which ranks the server's built examples by the
 * description while the person types. Open example creates the Project from a built example as it is, in
 * one click, for either agent: the request carries the example and no message, and the description waits
 * in the opened Project's composer as an unsent draft. A placeholder fills the composer. The Project's name
 * follows from the description, a picked placeholder's title or the opened example's title. Agent is chosen
 * before the model; only the Superverse agent has a model choice. Advanced holds the editable name. Nothing
 * is sent here: the creation intent carries the first message or the example, and the agent choice, to the
 * next page.
 */
export function ProjectStartForm({ teamSpaceId }: { teamSpaceId: string }) {
  const router = useRouter();
  const mockScenarios = useMockScenarios();
  const [description, setDescription] = useState("");
  const [picked, setPicked] = useState<ProcessTemplate | null>(null);
  // Null until the person edits the name: until then it follows the description.
  const [editedName, setEditedName] = useState<string | null>(null);
  const [chosen, setChosen] = useState<AgentModel | null>(null);
  const [executor, setExecutor] = useState<ProjectExecutor>(PROJECT_EXECUTOR.SUPERVERSE);
  const startingModel = useStartingModel(mockScenarios);
  // Null only while the administrator's offer of the mock model is being read.
  const model = chosen ?? startingModel;
  const [formError, setFormError] = useState<string | null>(null);
  const [transitioning, setTransitioning] = useState(false);
  const composer = useRef<HTMLTextAreaElement | null>(null);
  const examples = useExamples();
  // A picked placeholder names the Project while the description still opens as the placeholder does.
  const placeholderOf = (text: string) =>
    picked && projectNameFrom(text) === projectNameFrom(picked.description) ? picked : null;
  const derivedName = (text: string) => placeholderOf(text)?.title ?? (projectNameFrom(text) || FALLBACK_NAME);
  const name = editedName ?? (description.trim() ? derivedName(description) : "");

  useEffect(() => {
    if (hasPendingProjectCreation() || readProjectIntent()) router.replace(studioRoutes.projectCreation);
  }, [router]);

  function pick(template: ProcessTemplate) {
    setPicked(template);
    setDescription(template.description);
    composer.current?.focus();
  }

  /**
   * Stores the creation and moves to the creation page; throws when it cannot start, so that the composer
   * keeps what the person wrote. A creation from an example carries no message: `draft` waits in the
   * opened Project's composer.
   */
  function storeCreation(projectName: string, prompt: string, from?: { example: BuiltExample; draft: string }) {
    if (executor === PROJECT_EXECUTOR.SUPERVERSE && model === null) throw new Error(READING_MODELS);
    if (!projectName) {
      setFormError("Give the project a name first, under Advanced");
      throw new Error("The project has no name");
    }
    try {
      storePendingProjectCreation({
        teamSpaceId,
        name: projectName,
        prompt,
        executor,
        model: executor === PROJECT_EXECUTOR.SUPERVERSE ? model : null,
        idempotencyKey: newRequestId(),
        messageIdempotencyKey: newRequestId(),
        ...(from ? { example: { exampleId: from.example.id } } : {}),
        ...(from?.draft ? { draft: from.draft } : {}),
      });
    } catch (reason) {
      setFormError(reason instanceof Error ? reason.message : "Could not keep the project request in this browser");
      throw reason;
    }
    setTransitioning(true);
    router.push(studioRoutes.projectCreation);
  }

  async function beginCreation(prompt: string) {
    storeCreation(editedName === null ? derivedName(prompt) : editedName.trim(), prompt);
  }

  function openExample(example: BuiltExample) {
    try {
      storeCreation(editedName?.trim() || example.title, "", { example, draft: description.trim() });
    } catch {
      // The form shows why; the description stays.
    }
  }

  return (
    <main className="flex min-h-0 min-w-0 flex-1 items-center-safe justify-center-safe overflow-y-auto p-8 max-sm:p-4">
      <div className="w-full max-w-2xl py-8">
        <h1 className="text-center text-display font-semibold tracking-tight">What process should Dex run?</h1>
        <p className="mx-auto mt-3 max-w-xl text-center text-reading text-ink-dim">
          Describe it the way you&apos;d explain it to a new teammate: what starts it, who decides, and what happens
          when something goes wrong.
        </p>
        {formError && <Banner className="mt-4">{formError}</Banner>}
        <fieldset className="mt-6 rounded-lg border border-line-soft bg-panel p-3">
          <legend className="px-1 text-body font-medium text-ink">Agent</legend>
          <div className="grid gap-2 sm:grid-cols-2">
            <label className="flex cursor-pointer items-start gap-2 rounded-lg border border-line-soft p-3 text-reading text-ink-dim has-checked:border-accent-rule has-checked:bg-accent-wash">
              <input
                type="radio"
                name="start-agent"
                value={PROJECT_EXECUTOR.SUPERVERSE}
                checked={executor === PROJECT_EXECUTOR.SUPERVERSE}
                disabled={transitioning}
                onChange={() => setExecutor(PROJECT_EXECUTOR.SUPERVERSE)}
              />
              <span>
                <strong className="block font-medium text-ink">Superverse agent</strong>Runs in your Project workspace.
              </span>
            </label>
            <label className="flex cursor-pointer items-start gap-2 rounded-lg border border-line-soft p-3 text-reading text-ink-dim has-checked:border-accent-rule has-checked:bg-accent-wash">
              <input
                type="radio"
                name="start-agent"
                value={PROJECT_EXECUTOR.LOCAL}
                checked={executor === PROJECT_EXECUTOR.LOCAL}
                disabled={transitioning}
                onChange={() => setExecutor(PROJECT_EXECUTOR.LOCAL)}
              />
              <span>
                <strong className="block font-medium text-ink">My local agent</strong>Connect an agent on your computer
                after the Project is ready.
              </span>
            </label>
          </div>
          {executor === PROJECT_EXECUTOR.SUPERVERSE && (
            <StartingModelPicker
              className="mt-3 rounded-lg border border-line-soft"
              model={model}
              mockSelectable={mockScenarios?.selectable ?? false}
              disabled={transitioning || model === null}
              onSelect={setChosen}
            />
          )}
        </fieldset>
        <div className="mt-6">
          {executor === PROJECT_EXECUTOR.SUPERVERSE && model === "mock" ? (
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
              onAnswer={noPendingInput}
            />
          ) : (
            <Composer
              large
              autoFocus
              value={description}
              onChange={setDescription}
              inputRef={composer}
              busy={transitioning}
              disabledReason={executor === PROJECT_EXECUTOR.SUPERVERSE && model === null ? READING_MODELS : null}
              placeholder="For example: when a customer asks for a refund by email, a support lead approves it in Slack and Stripe pays it back…"
              onSend={beginCreation}
            />
          )}
        </div>
        <MoreDetails surface="start.advanced" label="Advanced" className="mt-3">
          <div className="grid gap-3 rounded-lg border border-line-soft bg-panel p-3">
            <label className="grid gap-1.5 text-body font-medium text-ink-dim">
              Project name
              <input
                value={name}
                maxLength={200}
                disabled={transitioning}
                onChange={(event) => setEditedName(event.target.value)}
                className={cx(
                  "w-full rounded-lg border border-line bg-panel px-3 py-2 text-reading font-normal text-ink placeholder:text-ink-faint focus:border-accent focus:shadow-(--focus-ring) focus:outline-none disabled:opacity-50",
                )}
                placeholder="Named from your description"
              />
            </label>
          </div>
        </MoreDetails>
        <ExampleGrid
          description={description}
          examples={examples}
          showPlaceholders={executor === PROJECT_EXECUTOR.LOCAL || model !== "mock"}
          picked={placeholderOf(description)?.id ?? null}
          disabled={transitioning || (executor === PROJECT_EXECUTOR.SUPERVERSE && model === null)}
          onOpen={openExample}
          onPick={pick}
        />
      </div>
    </main>
  );
}
