// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState } from "react";
import {
  MOCK_MODEL_API_KEY,
  ModelCredentialStatus,
  saveModelCredential,
  deleteModelCredential,
  listModelCredentialModels,
} from "@/lib/api/model-credential";
import { useSessionResource } from "@/lib/session/SessionProvider";
import type { useModelCredential } from "@/lib/session/credentials/use-model-credential";
import { CustomEndpointFields, customBudgetError } from "./CustomEndpointFields";
import type { StartingCredential } from "@/lib/session/credentials/starting-credential";
import { useSingleFlight } from "@/lib/browser/use-single-flight";

/** A write-only password field and ModelKeyInfo metadata; the saved key is never returned. */
export function ModelCredentialEntry({
  canManage,
  providerName = "Anthropic",
  keyHint = "Anthropic API key",
  credential: chosenCredential,
  mockEndpoint = false,
  allowCustom = false,
  stage,
  onLocked,
}: {
  onLocked?: (locked: boolean) => void;
  stage?: { provider: StartingCredential["provider"]; onSave: (value: StartingCredential) => void };
  canManage: boolean;
  providerName?: string;
  keyHint?: string;
  credential?: ReturnType<typeof useModelCredential>;
  mockEndpoint?: boolean;
  allowCustom?: boolean;
}) {
  const session = useSessionResource();
  const field = useRef<HTMLInputElement>(null);
  const endpointField = useRef<HTMLInputElement>(null);
  const modelField = useRef<HTMLInputElement>(null);
  const contextField = useRef<HTMLInputElement>(null);
  const outputField = useRef<HTMLInputElement>(null);
  const contextEdited = useRef(false);
  const hints = useRef(new Map<string, string>());
  const [dirty, setDirty] = useState(false);
  const [generation, setGeneration] = useState(0);
  const [notice, setNotice] = useState("");
  const [custom, setCustom] = useState(false);
  const [models, setModels] = useState<string[]>([]);
  const [discoveryNote, setDiscoveryNote] = useState("");
  const [editing, setEditing] = useState(false);
  const [confirm, setConfirm] = useState<"replace" | "delete" | null>(null);
  const action = useSingleFlight<"save" | "delete" | "discover">();
  useEffect(() => {
    onLocked?.(dirty || action.busy !== null);
  }, [dirty, action.busy, onLocked]);
  useEffect(() => () => onLocked?.(false), [onLocked]);
  const credential = chosenCredential ?? session?.credential;
  if (!credential || (!session && !stage)) return null;
  const projectId = session?.projectId ?? "";
  const info = credential.info;
  const running =
    session?.resource?.agent?.description?.subagents.filter(
      (agent) => !agent.lost && (agent.status === "STATUS_STARTING" || agent.status === "STATUS_RUNNING"),
    ).length ?? 0;
  const active = info?.status === ModelCredentialStatus.MODEL_CREDENTIAL_STATUS_ACTIVE;
  const save = (retry = false) => {
    const contextWindow = contextField.current?.value ?? "32768";
    const maxOutputTokens = outputField.current?.value || "4096";
    if (custom && !retry) {
      const error = customBudgetError(contextWindow, maxOutputTokens);
      if (error) {
        action.setError(error);
        return;
      }
    }
    const key = field.current?.value ?? "";
    if (stage) {
      if (!key.trim()) {
        action.setError("Enter an API key first.");
        return;
      }
      stage.onSave({
        provider: stage.provider,
        apiKey: key,
        endpoint: {
          baseUrl: custom ? (endpointField.current?.value ?? "") : "",
          customModelId: custom ? (modelField.current?.value ?? "") : "",
          contextWindow: custom ? contextWindow : "0",
          maxOutputTokens: custom ? maxOutputTokens : "0",
        },
      });
      setDirty(false);
      action.setError("");
      setDiscoveryNote("Key ready for the new Project. Build will validate it before sending.");
      return;
    }
    const operation = retry
      ? credential.pending
      : { kind: "save" as const, revision: info?.revision ?? "0", requestId: crypto.randomUUID() };
    if (!operation) return;
    setNotice("");
    credential.setPending(operation);
    setEditing(true);
    setConfirm(null);
    void action.run("save", async (signal) => {
      try {
        const next = await saveModelCredential(
          projectId,
          credential.provider,
          operation.revision,
          operation.requestId,
          retry ? "" : key,
          signal,
          {
            baseUrl: custom ? (endpointField.current?.value ?? "") : "",
            customModelId: custom ? (modelField.current?.value ?? "") : "",
            contextWindow: custom ? contextWindow : "0",
            maxOutputTokens: custom ? maxOutputTokens : "0",
          },
        );
        credential.setInfo(next.credential);
        setNotice(next.noticeMessage);
        if (field.current) field.current.value = "";
        setDirty(false);
        credential.setPending(null);
        setEditing(false);
      } catch (reason) {
        const latest = await credential.refresh();
        if (latest && latest.status !== ModelCredentialStatus.MODEL_CREDENTIAL_STATUS_PENDING)
          credential.setPending(null);
        throw reason;
      }
    });
  };
  const remove = (retry = false) => {
    const operation = retry
      ? credential.pending
      : { kind: "delete" as const, revision: info?.revision ?? "0", requestId: crypto.randomUUID() };
    if (!operation) return;
    credential.setPending(operation);
    setConfirm(null);
    void action.run("delete", async (signal) => {
      try {
        const next = await deleteModelCredential(
          projectId,
          credential.provider,
          operation.revision,
          operation.requestId,
          signal,
        );
        credential.setInfo(next);
        credential.setPending(null);
        setEditing(false);
      } catch (reason) {
        const latest = await credential.refresh();
        if (latest && latest.status !== ModelCredentialStatus.MODEL_CREDENTIAL_STATUS_PENDING)
          credential.setPending(null);
        throw reason;
      }
    });
  };
  const applyHint = (model: string) => {
    const hint = hints.current.get(model);
    if (!contextEdited.current && !info?.baseUrl && contextField.current) contextField.current.value = hint ?? "32768";
  };
  const clearDiscovery = () => {
    hints.current.clear();
    if (!contextEdited.current && !info?.baseUrl && contextField.current) contextField.current.value = "32768";
    setModels([]);
    setDiscoveryNote("");
  };
  const discover = () => {
    if (stage) {
      setDiscoveryNote(
        "Create the Project to discover models and validate this key before sending. You can also enter a model ID now.",
      );
      return;
    }
    const baseUrl = endpointField.current?.value ?? "";
    const key = field.current?.value ?? "";
    void action.run("discover", async (signal) => {
      const response = await listModelCredentialModels(
        projectId,
        credential.provider,
        baseUrl,
        key,
        info?.revision ?? "0",
        signal,
      );
      signal.throwIfAborted();
      if (endpointField.current?.value !== baseUrl) return;
      hints.current = new Map(
        (response.models ?? [])
          .filter((model) => {
            const value = Number(model.contextWindow);
            return (
              /^\d+$/.test(model.contextWindow) && Number.isSafeInteger(value) && value >= 32768 && value <= 1000000
            );
          })
          .map((model) => [model.modelId, model.contextWindow]),
      );
      setModels(response.modelIds);
      if (response.modelIds.length === 1 && modelField.current) {
        modelField.current.value = response.modelIds[0];
        applyHint(response.modelIds[0]);
      }
      setDiscoveryNote(
        response.modelIds.length > 1
          ? "Choose a model or enter its ID."
          : response.modelIds.length === 1
            ? "One model found."
            : "This endpoint has no model list. Enter a model ID.",
      );
    });
  };
  const change = (kind: "replace" | "delete") => {
    if (running > 0) {
      setConfirm(kind);
      return;
    }
    if (kind === "delete") remove();
    else save();
  };
  return (
    <div
      key={generation}
      className="my-1 grid gap-2 rounded-md border border-line p-2.25 text-xs"
      data-model-credential
      onChange={() => setDirty(true)}
    >
      <p className="font-medium text-ink">
        {custom || info?.baseUrl ? "Custom endpoint API key" : keyHint} ·{" "}
        {active
          ? `••••${info.last4}`
          : info?.status === ModelCredentialStatus.MODEL_CREDENTIAL_STATUS_PENDING
            ? "Pending reconciliation"
            : "Not set"}
      </p>
      {active && info.baseUrl && <p className="text-ink-faint">Custom endpoint · {info.effectiveModelId}</p>}
      {active && (info.setBy?.accountId || info.setBy?.email) && (
        <p className="text-ink-faint">
          Set by {info.setBy?.email || "a Builder"}
          {info.setAt ? ` · ${new Date(info.setAt).toLocaleString()}` : ""}
        </p>
      )}
      <p className="text-ink-faint">
        {mockEndpoint && !custom
          ? `This local endpoint accepts the test key ${MOCK_MODEL_API_KEY}. No model charge.`
          : `All Builders of this Project use this key. ${custom || info?.baseUrl ? "Your endpoint’s provider" : providerName} bills your account. Saving tests the key with a small billable request.`}
      </p>
      {stage && (
        <p className="text-ink-faint">
          Kept only in memory until this Project is created. Reloading requires entering the key again.
        </p>
      )}
      {canManage && (!credential.pending || dirty || action.busy !== null) && (
        <>
          {(!active || editing) && (
            <>
              <label className="grid gap-1">
                {custom ? "Custom endpoint API key" : keyHint}
                <input
                  ref={field}
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  disabled={action.busy !== null}
                  className="h-8 rounded border border-line bg-panel px-2 text-ink"
                />
              </label>
              {allowCustom && (
                <>
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      checked={custom}
                      disabled={action.busy !== null}
                      onChange={(event) => {
                        setCustom(event.target.checked);
                        clearDiscovery();
                      }}
                    />
                    Use a custom endpoint
                  </label>
                  {custom && (
                    <CustomEndpointFields
                      endpointField={endpointField}
                      modelField={modelField}
                      contextField={contextField}
                      outputField={outputField}
                      info={info}
                      busy={action.busy}
                      models={models}
                      discoveryNote={discoveryNote}
                      provider={credential.provider}
                      discover={discover}
                      clearDiscovery={clearDiscovery}
                      chooseModel={applyHint}
                      editContext={() => {
                        contextEdited.current = true;
                      }}
                    />
                  )}
                </>
              )}
              <button
                type="button"
                disabled={action.busy !== null || (!stage && (info === null || credential.pending !== null))}
                onClick={() => change("replace")}
                className="rounded border border-accent-rule px-2 py-1 text-accent hover:bg-accent-subtle"
              >
                {action.busy === "save" ? "Saving…" : stage ? "Use this key for the new Project" : "Save key"}
              </button>
            </>
          )}
          {active && !editing && (
            <button
              type="button"
              onClick={() => {
                setCustom(!!info?.baseUrl);
                contextEdited.current = false;
                clearDiscovery();
                setEditing(true);
              }}
              className="text-left text-accent"
            >
              Replace key
            </button>
          )}
          {active && (
            <button
              type="button"
              disabled={action.busy !== null}
              onClick={() => change("delete")}
              className="text-left text-fail"
            >
              Delete key
            </button>
          )}
          {confirm && (
            <div role="alert" className="grid gap-1 text-ink">
              <p>Running subagents that use this provider will stop because its key changed.</p>
              <button type="button" onClick={() => (confirm === "delete" ? remove() : save())}>
                Confirm {confirm}
              </button>
              <button type="button" onClick={() => setConfirm(null)}>
                Cancel
              </button>
            </div>
          )}
        </>
      )}
      {dirty && (
        <button
          type="button"
          disabled={action.busy !== null || !!credential.pending}
          onClick={() => {
            setDirty(false);
            setEditing(false);
            setCustom(false);
            setGeneration((value) => value + 1);
            clearDiscovery();
            action.setError("");
          }}
        >
          Discard changes
        </button>
      )}
      {notice && <p role="status">{notice}</p>}
      {canManage && credential.pending && action.busy === null && (
        <button
          type="button"
          disabled={action.busy !== null}
          onClick={() => (credential.pending?.kind === "delete" ? remove(true) : save(true))}
        >
          Finish pending change
        </button>
      )}
      {(action.error || credential.error) && (
        <p role="alert" className="text-fail">
          {action.error || credential.error}
        </p>
      )}
    </div>
  );
}
