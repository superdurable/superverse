// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef, useState, type FormEvent } from "react";
import type { FlowV2StartDefinition } from "@/lib/api/flow/flow-definition";
import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import { recoverFlowStart, startFlow, type FlowStart } from "@/lib/api/flow/flow-run";
import type { StartFlowCapability } from "@/lib/api/flow/flow-types";
import { useFlowApi } from "@/lib/flow/FlowApiProvider";
import { SchemaInput } from "./SchemaInput";
import { createStartInputDraft, serializeStartInput, type StartInputError } from "./start-input";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { START_FIELDSET, START_CAPTION } from "./SchemaInput";

export function StartFlowDialog({
  definition,
  definitionRevision,
  flowType,
  target,
  live = false,
  onClose,
  onDefinitionChanged,
  onStarted,
}: {
  definition: FlowV2StartDefinition;
  definitionRevision: string;
  flowType: string;
  target: StartFlowCapability;
  /** A run on Live: real data and real people, so the dialog says so before it starts. */
  live?: boolean;
  onClose: () => void;
  onDefinitionChanged: (error: unknown) => boolean;
  onStarted: (flowID: string) => void;
}) {
  const client = useFlowApi();
  const [flowID] = useState(() => `flow-${crypto.randomUUID()}`);
  const [requestID] = useState(() => crypto.randomUUID());
  const [draft, setDraft] = useState(() => createStartInputDraft(definition.input));
  const [errors, setErrors] = useState<StartInputError[]>([]);
  const [submitError, setSubmitError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const requestController = useRef<AbortController | null>(null);
  const acceptedRequest = useRef<FlowStart | null>(null);
  const [isRecoveryPending, setRecoveryPending] = useState(false);

  useEffect(() => {
    dialogRef.current?.querySelector<HTMLInputElement>("input, textarea, select")?.focus();
    return () => {
      requestController.current?.abort();
    };
  }, []);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    const input = serializeStartInput(definition.input, draft);
    setErrors(input.errors);
    setSubmitError("");
    if (input.errors.length > 0) {
      focusPath(dialogRef.current, input.errors[0].path);
      return;
    }
    setSubmitting(true);
    const controller = new AbortController();
    requestController.current?.abort();
    requestController.current = controller;
    // A recovery resends the accepted request unchanged, so the server can match the earlier one.
    const request = acceptedRequest.current ?? {
      flowType,
      flowId: flowID,
      requestId: requestID,
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
      if (!controller.signal.aborted) onStarted(result.flowId);
    } catch (error) {
      if (controller.signal.aborted) return;
      if (onDefinitionChanged(error)) {
        onClose();
        return;
      }
      // Without a definite answer the Flow may have started, so the next submit recovers the same
      // request instead of starting another; a definite rejection frees the input for editing.
      const isUncertain = !(error instanceof ApiError) || error.status >= 500;
      setRecoveryPending(isUncertain);
      if (!isUncertain) acceptedRequest.current = null;
      setSubmitError(errorMessage(error, "Start Flow failed"));
    } finally {
      if (!controller.signal.aborted) setSubmitting(false);
    }
  };

  return (
    <Dialog
      aria-labelledby="start-flow-title"
      width="lg"
      ref={dialogRef}
      onDismiss={() => {
        if (!submitting) onClose();
      }}
    >
      {/* The header and the actions stay put while the input between them scrolls. */}
      <form className="flex max-h-[calc(100dvh-2rem-2px)] flex-col" onSubmit={(event) => void submit(event)}>
        <header className="flex items-center gap-2.5 border-b border-line-faint px-4.5 py-3.5">
          <div>
            <h2 className="m-0 text-[17px]" id="start-flow-title">
              {live ? "Start a live run of" : "Start a test run of"} {flowType}
            </h2>
            <p className="mt-0.75 font-mono text-[11px] text-ink-faint">{definition.stepType}</p>
          </div>
          <Button
            aria-label="Close Start Flow"
            className="ml-auto"
            disabled={submitting}
            variant="quiet"
            size="icon"
            onClick={onClose}
          >
            ×
          </Button>
        </header>
        <div className="grid gap-3 overflow-auto p-4.5">
          <fieldset className={START_FIELDSET} disabled={submitting || isRecoveryPending}>
            <legend className={START_CAPTION}>Start input</legend>
            <SchemaInput
              draft={draft}
              errors={errors}
              label="Input"
              path="$"
              required
              schema={definition.input}
              onChange={setDraft}
            />
          </fieldset>
          {live && (
            <Banner tone="busy" role="note">
              It runs on Live with real data and real people, exactly like a run started any other way. Use a test run
              on Preview to try things.
            </Banner>
          )}
          {submitError && <Banner role="alert">{submitError}</Banner>}
        </div>
        <footer className="flex items-center justify-end gap-2.5 border-t border-line-faint px-4.5 py-3.5">
          <Button disabled={submitting} variant="quiet" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={submitting} variant="primary" type="submit">
            {submitting ? "Starting…" : isRecoveryPending ? "Recover Start" : live ? "Start live run" : "Start Flow"}
          </Button>
        </footer>
      </form>
    </Dialog>
  );
}

function focusPath(dialog: HTMLDialogElement | null, path: string) {
  const element = Array.from(dialog?.querySelectorAll<HTMLElement>("[data-start-path]") ?? []).find(
    (candidate) => candidate.dataset.startPath === path,
  );
  element?.focus();
}
