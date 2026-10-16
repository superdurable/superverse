// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import type { FlowV2StartDefinition } from "@/lib/api/flow/flow-definition";
import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import { recoverFlowStart, startFlow, type FlowStart } from "@/lib/api/flow/flow-run";
import type { StartFlowCapability } from "@/lib/api/flow/flow-types";
import { useFlowApi } from "@/lib/flow/FlowApiProvider";
import { StartFields } from "./SchemaInput";
import { createStartInputDraft, serializeStartInput, type StartInputError } from "./start-input";
import { startedWords } from "./start-labels";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { MoreDetails } from "@/components/ui/reveal/MoreDetails";

/** A new start's identity: the Flow ID it asks for and the request that admits it once. */
const newIdentity = () => ({ flowID: `flow-${crypto.randomUUID()}`, requestID: crypto.randomUUID() });

/**
 * The start form of one process, in a dialog or inline: its input as a business form, then Start test run
 * (Preview) or Start live run (Live). A start whose answer was lost is never sent as a new one: the next
 * submit asks whether the same request started a run, and starts it only when it did not.
 */
export function StartForm({
  definition,
  definitionRevision,
  flowType,
  target,
  live = false,
  layout,
  header,
  cancel,
  autoFocus = false,
  onBusyChange,
  onDefinitionChanged,
  onStarted,
}: {
  definition: FlowV2StartDefinition;
  definitionRevision: string;
  flowType: string;
  target: StartFlowCapability;
  /** A run on Live: real data and real people, so the form says so before it starts. */
  live?: boolean;
  /** A dialog's header, scrolling body and footer, or one compact stack. */
  layout: "dialog" | "inline";
  header?: ReactNode;
  /** Beside the start button, such as the dialog's Cancel. */
  cancel?: ReactNode;
  autoFocus?: boolean;
  onBusyChange?: (busy: boolean) => void;
  /** Whether the refusal was a changed definition, which the caller then shows instead. */
  onDefinitionChanged: (error: unknown) => boolean;
  /** The started run, and the words that say so: "Started a test run of Text summary." */
  onStarted: (flowID: string, words: string) => void;
}) {
  const client = useFlowApi();
  const [identity, setIdentity] = useState(newIdentity);
  const [draft, setDraft] = useState(() => createStartInputDraft(definition.input));
  const [errors, setErrors] = useState<StartInputError[]>([]);
  const [submitError, setSubmitError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const formRef = useRef<HTMLFormElement>(null);
  const requestController = useRef<AbortController | null>(null);
  const acceptedRequest = useRef<FlowStart | null>(null);
  const [isRecoveryPending, setRecoveryPending] = useState(false);

  useEffect(() => {
    if (autoFocus)
      formRef.current
        ?.querySelector("fieldset")
        ?.querySelector<HTMLElement>("input, textarea, select, button")
        ?.focus();
    return () => {
      requestController.current?.abort();
    };
  }, [autoFocus]);
  useEffect(() => onBusyChange?.(submitting), [onBusyChange, submitting]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const input = serializeStartInput(definition.input, draft);
    setErrors(input.errors);
    setSubmitError("");
    if (input.errors.length > 0) {
      focusPath(formRef.current, input.errors[0].path);
      return;
    }
    setSubmitting(true);
    const controller = new AbortController();
    requestController.current?.abort();
    requestController.current = controller;
    // A recovery resends the accepted request unchanged, so the server can match the earlier one.
    const request = acceptedRequest.current ?? {
      flowType,
      flowId: identity.flowID,
      requestId: identity.requestID,
      targetRevision: target.targetRevision,
      inputJson: input.json,
    };
    acceptedRequest.current = request;
    const options = { definitionRevision, signal: controller.signal };
    const start = () => startFlow(client, request, options);
    // START_NOT_FOUND means the uncertain earlier request never started the Flow, so start it now.
    const recoverOrStart = () =>
      recoverFlowStart(client, request, options).catch((error: unknown) => {
        if (error instanceof ApiError && error.code === "START_NOT_FOUND") return start();
        throw error;
      });
    try {
      const result = isRecoveryPending ? await recoverOrStart() : await start();
      if (controller.signal.aborted) return;
      // The next start from this form is a new run with its own identity.
      acceptedRequest.current = null;
      setIdentity(newIdentity());
      setRecoveryPending(false);
      onStarted(result.flowId, startedWords(flowType, live));
    } catch (error) {
      if (controller.signal.aborted) return;
      if (onDefinitionChanged(error)) return;
      // Without a definite answer the Flow may have started, so the next submit recovers the same
      // request instead of starting another; a definite rejection frees the input for editing.
      const isUncertain = !(error instanceof ApiError) || error.status >= 500;
      setRecoveryPending(isUncertain);
      if (!isUncertain) acceptedRequest.current = null;
      const reason = errorMessage(error, "The run did not start.");
      setSubmitError(
        isUncertain
          ? `${reason} It may have started anyway. Try again checks first, so the run never starts twice.`
          : reason,
      );
    } finally {
      if (!controller.signal.aborted) setSubmitting(false);
    }
  };

  const dialog = layout === "dialog";
  const label = submitting ? "Starting…" : isRecoveryPending ? "Try again" : live ? "Start live run" : "Start test run";
  return (
    <form
      ref={formRef}
      className={dialog ? "flex max-h-[calc(100dvh-2rem-2px)] flex-col" : "grid gap-3"}
      aria-label={`Start a ${live ? "live" : "test"} run`}
      onSubmit={(event) => void submit(event)}
    >
      {header}
      {/* In a dialog the header and the actions stay put while the input between them scrolls. */}
      <div className={dialog ? "grid gap-3 overflow-auto p-4.5" : "grid gap-3"}>
        <fieldset className="m-0 grid min-w-0 gap-3 border-0 p-0" disabled={submitting || isRecoveryPending}>
          <StartFields schema={definition.input} draft={draft} errors={errors} onChange={setDraft} />
        </fieldset>
        <MoreDetails surface="runs.start">
          <p className="text-caption text-ink-faint">
            Process <code className="font-mono text-ink-dim">{flowType}</code>, starting at{" "}
            <code className="font-mono text-ink-dim">{definition.stepType}</code>.
          </p>
        </MoreDetails>
        {live && (
          <Banner tone="busy" role="note">
            It runs on Live with real data and real people, exactly like a run started any other way. Use a test run on
            Preview to try things.
          </Banner>
        )}
        {submitError && <Banner role="alert">{submitError}</Banner>}
      </div>
      <footer
        className={cx("flex items-center gap-2.5", dialog && "justify-end border-t border-line-faint px-4.5 py-3.5")}
      >
        {cancel}
        <Button disabled={submitting} variant="primary" type="submit">
          {label}
        </Button>
      </footer>
    </form>
  );
}

function focusPath(form: HTMLFormElement | null, path: string) {
  const element = Array.from(form?.querySelectorAll<HTMLElement>("[data-start-path]") ?? []).find(
    (candidate) => candidate.dataset.startPath === path,
  );
  element?.focus();
}
