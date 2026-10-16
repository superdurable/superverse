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
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";
import { projectNameFrom } from "./templates/project-name";
import type { ProcessTemplate } from "./templates/template-catalog";
import { TemplateGallery } from "./templates/TemplateGallery";

/** The name of a Project whose description gives none, such as one of punctuation only. */
const FALLBACK_NAME = "New process";

/** The first message is the first input of a scenario, which answers no pending input. */
function noPendingInput(): Promise<never> {
  throw new Error("The first message answers no pending input");
}

const READING_MODELS = "Reading the model options…";

/**
 * The start page: the description of the process, which becomes the first message of the Project's
 * Session, with examples that fill it in. The Project's name follows from a picked example's title or
 * from the description; Advanced holds the name to change it and the model of the Session, preselected
 * on the model a new Session starts on: Sonnet, or the mock model for the platform administrator where
 * the server offers it. On the mock model the composer takes the first input of a scenario in place of
 * free text. Nothing is sent here: the choice is stored with the creation, and Build applies the model
 * and sends the message once the Session starts.
 */
export function ProjectStartForm({ teamSpaceId }: { teamSpaceId: string }) {
  const router = useRouter();
  const mockScenarios = useMockScenarios();
  const [description, setDescription] = useState("");
  const [picked, setPicked] = useState<ProcessTemplate | null>(null);
  // Null until the person edits the name: until then it follows the description.
  const [editedName, setEditedName] = useState<string | null>(null);
  const [chosen, setChosen] = useState<AgentModel | null>(null);
  const startingModel = useStartingModel(mockScenarios);
  // Null only while the administrator's offer of the mock model is being read.
  const model = chosen ?? startingModel;
  const [formError, setFormError] = useState<string | null>(null);
  const [transitioning, setTransitioning] = useState(false);
  const composer = useRef<HTMLTextAreaElement | null>(null);
  // A picked example names the Project while the description still opens as the example does.
  const exampleOf = (text: string) =>
    picked && projectNameFrom(text) === projectNameFrom(picked.description) ? picked : null;
  const derivedName = (text: string) => exampleOf(text)?.title ?? (projectNameFrom(text) || FALLBACK_NAME);
  const name = editedName ?? (description.trim() ? derivedName(description) : "");

  useEffect(() => {
    if (hasPendingProjectCreation()) router.replace(studioRoutes.projectCreation);
  }, [router]);

  function pick(template: ProcessTemplate) {
    setPicked(template);
    setDescription(template.description);
    composer.current?.focus();
  }

  // Rejects when the creation cannot start, so that the composer keeps what the person wrote.
  async function beginCreation(prompt: string, mock: MockInputChoice | null = null) {
    const projectName = editedName === null ? derivedName(prompt) : editedName.trim();
    if (model === null) throw new Error(READING_MODELS);
    if (!projectName) {
      setFormError("Give the project a name first, under Advanced");
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
        <h1 className="text-center text-display font-semibold tracking-tight">What process should Dex run?</h1>
        <p className="mx-auto mt-3 max-w-xl text-center text-reading text-ink-dim">
          Describe it the way you&apos;d explain it to a new teammate: what starts it, who decides, and what happens
          when something goes wrong.
        </p>
        {formError && <Banner className="mt-4">{formError}</Banner>}
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
              value={description}
              onChange={setDescription}
              inputRef={composer}
              busy={transitioning}
              disabledReason={model === null ? READING_MODELS : null}
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
            <SessionModelPicker
              className="rounded-lg border border-line-soft"
              model={model}
              mockSelectable={mockScenarios?.selectable ?? false}
              disabled={transitioning || model === null}
              onSelect={setChosen}
            />
          </div>
        </MoreDetails>
        {model !== "mock" && (
          <TemplateGallery
            picked={exampleOf(description)?.id ?? null}
            disabled={transitioning || model === null}
            onPick={pick}
          />
        )}
      </div>
    </main>
  );
}
