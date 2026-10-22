// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState } from "react";
import { createRemoteAgentPairing } from "@/lib/api/agent";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { timestampToISO } from "@/lib/shared/protobuf";
import { Button } from "@/components/ui/Button";
import { LocalAgentInstructions } from "@/components/session/remoteagent/LocalAgentInstructions";

/** Inline pairing is scoped to this creation and regenerates a code when its ten-minute TTL ends. */
export function ProjectCreationPairing({ projectId }: { projectId: string }) {
  const [accepted, setAccepted] = useState(false);
  const [pairing, setPairing] = useState<RpcOutput<"CreateRemoteAgentPairing"> | null>(null);
  const [error, setError] = useState("");
  const [copyNotice, setCopyNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const controller = useRef<AbortController | null>(null);
  const version = useRef(0);
  useEffect(() => () => controller.current?.abort(), []);

  useEffect(() => {
    if (!accepted || !pairing) return;
    const expires = Date.parse(timestampToISO(pairing.expiresAt) ?? "");
    if (!Number.isFinite(expires)) return;
    const remaining = Math.max(0, expires - Date.now());
    const tick = window.setInterval(() => setNow(Date.now()), 1000);
    const timeout = window.setTimeout(() => {
      setPairing(null);
      setNow(Date.now());
    }, remaining);
    return () => {
      window.clearInterval(tick);
      window.clearTimeout(timeout);
    };
  }, [accepted, pairing]);

  useEffect(() => {
    if (!accepted || pairing || error || controller.current) return;
    const request = new AbortController();
    controller.current = request;
    const current = ++version.current;
    setBusy(true);
    void createRemoteAgentPairing(projectId, request.signal)
      .then(
        (result) => {
          if (!request.signal.aborted && current === version.current) {
            setPairing(result);
            setCopyNotice("");
            setNow(Date.now());
          }
        },
        (reason: unknown) => {
          if (!request.signal.aborted && current === version.current)
            setError(reason instanceof Error ? reason.message : "Could not create a pairing code");
        },
      )
      .finally(() => {
        if (controller.current === request) controller.current = null;
        if (!request.signal.aborted && current === version.current) setBusy(false);
      });
  }, [accepted, error, pairing, projectId]);

  const expires = Date.parse(timestampToISO(pairing?.expiresAt) ?? "");
  const seconds = Number.isFinite(expires) ? Math.max(0, Math.ceil((expires - now) / 1000)) : 0;
  return (
    <section className="w-full rounded-lg border border-line-soft bg-panel p-5" aria-label="Connect your local agent">
      <h2 className="text-title font-medium">Connect your local agent</h2>
      <p className="mt-2 text-reading text-ink-dim">Your first message is waiting here until your agent connects.</p>
      <p id="local-agent-risk" className="mt-3 rounded-lg border border-line-soft bg-raised p-3 text-sm text-ink-dim">
        The local agent (Codex or Claude Code started by dexai) runs on your computer with that agent&apos;s own
        permissions. dexai limits Claude Code to platform tools. Project messages and application failure text reach it.
      </p>
      <label className="mt-3 flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          checked={accepted}
          onChange={(event) => {
            if (!event.target.checked) {
              controller.current?.abort();
              controller.current = null;
              version.current++;
              setPairing(null);
              setBusy(false);
            }
            setAccepted(event.target.checked);
          }}
          aria-describedby="local-agent-risk"
        />
        I understand these local-machine permissions.
      </label>
      {busy && (
        <p className="mt-3 text-sm text-ink-dim" role="status">
          Creating a pairing code…
        </p>
      )}
      {error && (
        <div className="mt-3 text-sm text-fail" role="alert">
          {error}
          <Button size="sm" onClick={() => setError("")}>
            Try again
          </Button>
        </div>
      )}
      {pairing && seconds > 0 && (
        <div className="mt-4 grid gap-2">
          <code aria-label="Pairing code" className="text-xl font-semibold tracking-widest select-all">
            {pairing.code}
          </code>
          <Button
            size="sm"
            className="justify-self-start"
            onClick={() => {
              void navigator.clipboard.writeText(pairing.code).then(
                () => setCopyNotice("Pairing code copied."),
                () => setCopyNotice("Copy failed. Select and copy the code manually."),
              );
            }}
          >
            Copy code
          </Button>
          {copyNotice && (
            <p className="text-xs text-ink-dim" role="status">
              {copyNotice}
            </p>
          )}
          <p className="text-sm text-ink-dim" role="status">
            Waiting for your agent to connect… Code expires in {Math.floor(seconds / 60)}:
            {String(seconds % 60).padStart(2, "0")}.
          </p>
        </div>
      )}
      <LocalAgentInstructions endpoint={pairing?.endpoint ?? null} awaitingAcknowledgement={!accepted} />
    </section>
  );
}
