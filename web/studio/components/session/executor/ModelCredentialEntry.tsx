// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRef, useState } from "react";
import { ModelCredentialStatus, saveModelCredential, deleteModelCredential } from "@/lib/api/model-credential";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { useSingleFlight } from "@/lib/browser/use-single-flight";

/** A write-only password field and ModelKeyInfo metadata; the saved key is never returned. */
export function ModelCredentialEntry({ canManage }: { canManage: boolean }) {
  const session = useSessionResource();
  const field = useRef<HTMLInputElement>(null);
  const [editing, setEditing] = useState(false);
  const [confirm, setConfirm] = useState<"replace" | "delete" | null>(null);
  const action = useSingleFlight<"save" | "delete">();
  if (!session) return null;
  const credential = session.credential,
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
          operation.revision,
          operation.requestId,
          retry ? "" : key,
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
  const remove = (retry = false) => {
    const operation = retry
      ? credential.pending
      : { kind: "delete" as const, revision: info?.revision ?? "0", requestId: crypto.randomUUID() };
    if (!operation) return;
    credential.setPending(operation);
    setConfirm(null);
    void action.run("delete", async (signal) => {
      try {
        const next = await deleteModelCredential(session.projectId, operation.revision, operation.requestId, signal);
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
        Anthropic API key ·{" "}
        {active
          ? `••••${info.last4}`
          : info?.status === ModelCredentialStatus.MODEL_CREDENTIAL_STATUS_PENDING
            ? "Pending reconciliation"
            : "Not set"}
      </p>
      {active && (info.setBy?.accountId || info.setBy?.email) && (
        <p className="text-ink-faint">
          Set by {info.setBy?.email || "a Builder"}
          {info.setAt ? ` · ${new Date(info.setAt).toLocaleString()}` : ""}
        </p>
      )}
      <p className="text-ink-faint">
        All Builders of this Project use this key. Anthropic bills your account. Saving tests the key with a small
        billable request.
      </p>
      {canManage && !credential.pending && (
        <>
          {(!active || editing) && (
            <>
              <label className="grid gap-1">
                Anthropic API key
                <input
                  ref={field}
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  disabled={action.busy !== null}
                  className="h-8 rounded border border-line bg-panel px-2 text-ink"
                />
              </label>
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
            <button type="button" onClick={() => setEditing(true)} className="text-left text-accent">
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
              <p>This will stop {running} running subagents because the model credential changed.</p>
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
