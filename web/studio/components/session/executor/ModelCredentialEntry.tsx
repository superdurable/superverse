// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef, useState } from "react";
import {
  MOCK_MODEL_API_KEY,
  ModelCredentialStatus,
  saveModelCredential,
  deleteModelCredential,
  listModelCredentialModels,
} from "@/lib/api/model-credential";
import { useSessionResource } from "@/lib/session/SessionProvider";
import type { useModelCredential } from "@/lib/session/credentials/use-model-credential";
import { useSingleFlight } from "@/lib/browser/use-single-flight";

/** A write-only password field and ModelKeyInfo metadata; the saved key is never returned. */
export function ModelCredentialEntry({
  canManage,
  providerName = "Anthropic",
  keyHint = "Anthropic API key",
  credential: chosenCredential,
  mockEndpoint = false,
  allowCustom = false,
}: {
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
  const [custom, setCustom] = useState(false);
  const [models, setModels] = useState<string[]>([]);
  const [discoveryNote, setDiscoveryNote] = useState("");
  const [editing, setEditing] = useState(false);
  const [confirm, setConfirm] = useState<"replace" | "delete" | null>(null);
  const action = useSingleFlight<"save" | "delete" | "discover">();
  if (!session) return null;
  const credential = chosenCredential ?? session.credential,
    info = credential.info;
  const running =
    session.resource?.agent?.description?.subagents.filter(
      (agent) => !agent.lost && (agent.status === "STATUS_STARTING" || agent.status === "STATUS_RUNNING"),
    ).length ?? 0;
  const active = info?.status === ModelCredentialStatus.MODEL_CREDENTIAL_STATUS_ACTIVE;
  const save = (retry = false) => {
    const key = field.current?.value ?? "";
    if (field.current) field.current.value = "";
    const operation = retry
      ? credential.pending
      : { kind: "save" as const, revision: info?.revision ?? "0", requestId: crypto.randomUUID() };
    if (!operation) return;
    credential.setPending(operation);
    setConfirm(null);
    void action.run("save", async (signal) => {
      try {
        const next = await saveModelCredential(
          session.projectId,
          credential.provider,
          operation.revision,
          operation.requestId,
          retry ? "" : key,
          signal,
          {
            baseUrl: custom ? (endpointField.current?.value ?? "") : "",
            customModelId: custom ? (modelField.current?.value ?? "") : "",
          },
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
          session.projectId,
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
  const discover = () => {
    const baseUrl = endpointField.current?.value ?? "";
    const key = field.current?.value ?? "";
    void action.run("discover", async (signal) => {
      const response = await listModelCredentialModels(
        session.projectId,
        credential.provider,
        baseUrl,
        key,
        info?.revision ?? "0",
        signal,
      );
      signal.throwIfAborted();
      setModels(response.modelIds);
      if (response.modelIds.length === 1 && modelField.current) modelField.current.value = response.modelIds[0];
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
    <div className="my-1 grid gap-2 rounded-md border border-line p-2.25 text-xs" data-model-credential>
      <p className="font-medium text-ink">
        {keyHint} ·{" "}
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
          : `All Builders of this Project use this key. ${providerName} bills your account. Saving tests the key with a small billable request.`}
      </p>
      {canManage && !credential.pending && (
        <>
          {(!active || editing) && (
            <>
              <label className="grid gap-1">
                {keyHint}
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
                        setModels([]);
                        setDiscoveryNote("");
                      }}
                    />
                    Use a custom endpoint
                  </label>
                  {custom && (
                    <>
                      <label className="grid gap-1">
                        Endpoint API base URL
                        <input
                          ref={endpointField}
                          type="url"
                          maxLength={2048}
                          defaultValue={info?.baseUrl ?? ""}
                          disabled={action.busy !== null}
                          className="h-8 rounded border border-line bg-panel px-2 text-ink"
                          onChange={() => {
                            setModels([]);
                            setDiscoveryNote("");
                          }}
                        />
                      </label>
                      <label className="grid gap-1">
                        Model ID (optional)
                        <input
                          ref={modelField}
                          maxLength={256}
                          defaultValue={info?.customModelId ?? ""}
                          list={`model-ids-${credential.provider}`}
                          disabled={action.busy !== null}
                          className="h-8 rounded border border-line bg-panel px-2 text-ink"
                        />
                      </label>
                      {models.length > 1 && (
                        <label className="grid gap-1">
                          Discovered models
                          <select
                            aria-label="Discovered models"
                            defaultValue=""
                            disabled={action.busy !== null}
                            className="h-8 rounded border border-line bg-panel px-2 text-ink"
                            onChange={(event) => {
                              if (modelField.current) modelField.current.value = event.target.value;
                            }}
                          >
                            <option value="">Choose a model</option>
                            {models.map((id) => (
                              <option key={id} value={id}>
                                {id}
                              </option>
                            ))}
                          </select>
                        </label>
                      )}
                      <datalist id={`model-ids-${credential.provider}`}>
                        {models.map((id) => (
                          <option key={id} value={id} />
                        ))}
                      </datalist>
                      <button
                        type="button"
                        disabled={action.busy !== null}
                        onClick={discover}
                        className="text-left text-accent"
                      >
                        {action.busy === "discover" ? "Discovering…" : "Discover models"}
                      </button>
                      <p className="text-ink-faint">
                        Leave the model ID empty to use the only discovered model. Text and serial tools only. Your
                        endpoint receives the conversation.
                      </p>
                      {discoveryNote && <p role="status">{discoveryNote}</p>}
                    </>
                  )}
                </>
              )}
              <button
                type="button"
                disabled={action.busy !== null || info === null}
                onClick={() => change("replace")}
                className="rounded border border-accent-rule px-2 py-1 text-accent hover:bg-accent-subtle"
              >
                {action.busy === "save" ? "Saving…" : "Save key"}
              </button>
            </>
          )}
          {active && !editing && (
            <button
              type="button"
              onClick={() => {
                setCustom(!!info?.baseUrl);
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
      {canManage && credential.pending && (
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
