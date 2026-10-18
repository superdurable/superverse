// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useId, useRef, useState } from "react";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { createRemoteAgentPairing, listRemoteAgentConnections, revokeRemoteAgentConnection } from "@/lib/api/agent";
import { timestampToISO } from "@/lib/shared/protobuf";
import type { RpcOutput } from "@/lib/shared/rpc-methods";

const connectorName = "dexai-connect";

/** Studio owns transport; the shared Agent conversation stays transport-free. */
export function RemoteAgentDialog({ projectId, onClose }: { projectId: string; onClose: () => void }) {
  return <ProjectConnectionDialog key={projectId} projectId={projectId} onClose={onClose} />;
}

// A new Project always gets new acknowledgement, code and display state, even
// if a future owner mounts the outer dialog without its own Project key.
function ProjectConnectionDialog({ projectId, onClose }: { projectId: string; onClose: () => void }) {
  const title = useId();
  const risk = useId();
  const request = useRef<AbortController | null>(null);
  const version = useRef(0);
  const [connections, setConnections] = useState<RpcOutput<"ListRemoteAgentConnections"> | null>(null);
  const [pairing, setPairing] = useState<RpcOutput<"CreateRemoteAgentPairing"> | null>(null);
  const [accepted, setAccepted] = useState(false);
  const [pending, setPending] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [now, setNow] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    request.current = controller;
    const current = ++version.current;
    void listRemoteAgentConnections(projectId, controller.signal)
      .then(
        (response) => {
          if (!controller.signal.aborted && current === version.current) setConnections(response);
        },
        (reason: unknown) => {
          if (!controller.signal.aborted && current === version.current) setError(message(reason));
        },
      )
      .finally(() => {
        if (!controller.signal.aborted && current === version.current) setPending(false);
      });
    return () => request.current?.abort();
  }, [projectId]);

  useEffect(() => {
    if (!pairing) return;
    const deadline = Date.parse(timestampToISO(pairing.expiresAt) ?? "");
    if (!Number.isFinite(deadline) || deadline <= Date.now()) return;
    const timer = setInterval(() => {
      const current = Date.now();
      setNow(current);
      if (current >= deadline) clearInterval(timer);
    }, 1000);
    return () => clearInterval(timer);
  }, [pairing]);

  const expiry = timestampToISO(pairing?.expiresAt);
  const seconds = expiry ? Math.max(0, Math.ceil((Date.parse(expiry) - now) / 1000)) : 0;
  const currentOwner = pairing ? pairing.currentOwner : connections?.currentOwner;
  const currentID = pairing?.currentConnectionId ?? connections?.currentConnectionId ?? "";
  const endpoint = pairing?.endpoint ?? connections?.endpoint ?? "";

  async function act(work: (signal: AbortSignal) => Promise<void>) {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    const current = ++version.current;
    setPending(true);
    setError("");
    setNotice("");
    try {
      await work(controller.signal);
    } catch (reason) {
      if (!controller.signal.aborted && current === version.current) setError(message(reason));
    } finally {
      if (!controller.signal.aborted && current === version.current) setPending(false);
    }
  }

  async function pair() {
    await act(async (signal) => {
      const response = await createRemoteAgentPairing(projectId, signal);
      signal.throwIfAborted();
      setNow(Date.now());
      setPairing(response);
    });
  }

  async function refresh() {
    await act(async (signal) => {
      const response = await listRemoteAgentConnections(projectId, signal);
      signal.throwIfAborted();
      setConnections(response);
      setPairing((previous) =>
        previous
          ? { ...previous, currentConnectionId: response.currentConnectionId, currentOwner: response.currentOwner }
          : null,
      );
    });
  }

  async function remove(connectionId: string) {
    await act(async (signal) => {
      await revokeRemoteAgentConnection(projectId, connectionId, signal);
      signal.throwIfAborted();
      setConnections((previous) =>
        previous
          ? { ...previous, connections: previous.connections.filter((item) => item.connectionId !== connectionId) }
          : null,
      );
      if (pairing?.connectionId === connectionId) setPairing(null);
      setNotice("Connection removed.");
    });
  }

  async function copyCode() {
    if (!pairing || seconds === 0) return;
    const current = version.current;
    const signal = request.current?.signal;
    try {
      await navigator.clipboard.writeText(pairing.code);
      if (!signal?.aborted && current === version.current) setNotice("Pairing code copied.");
    } catch {
      if (!signal?.aborted && current === version.current) setError("Copy failed. Select and copy the code manually.");
    }
  }

  const paired =
    pairing &&
    connections?.connections.some(
      (item) => item.connectionId === pairing.connectionId && item.status === "REMOTE_AGENT_CONNECTION_STATUS_PAIRED",
    );
  return (
    <Dialog onDismiss={onClose} aria-labelledby={title} className="p-5">
      <div className="mb-4 flex items-center justify-between gap-3">
        <h2 id={title} className="text-base font-semibold">
          Connect my agent
        </h2>
        <Button variant="quiet" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      <p className="mb-3 text-sm text-ink-dim">
        {!connections && !pairing
          ? "Current agent information is not available yet."
          : currentID
            ? `Current agent connected by ${currentOwner?.email ?? "another Builder"}. Activating your ${connectorName} stops their current work.`
            : "No remote agent is currently running this Session."}
      </p>
      <p id={risk} className="mb-3 rounded-lg border border-line-soft bg-raised p-3 text-sm text-ink-dim">
        The connected Codex runs on the connecting Builder&apos;s machine with the permissions that Builder gave it in
        Orca, and any Builder&apos;s message, and the failure text of an application Run, reaches it. A message could
        ask it to run something on that machine.
      </p>
      <label className="mb-4 flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          checked={accepted}
          onChange={(event) => setAccepted(event.target.checked)}
          aria-describedby={risk}
        />
        I understand these local-machine permissions.
      </label>
      {endpoint && (
        <p className="mb-3 text-xs break-all text-ink-faint">
          Endpoint: <code>{endpoint}</code>
        </p>
      )}
      <Button
        variant="primary"
        disabled={!accepted || pending || !connections || Boolean(pairing && seconds > 0)}
        onClick={() => void pair()}
      >
        Create pairing code
      </Button>
      {pairing && (
        <div className="mt-4 rounded-lg border border-line-soft p-3">
          {seconds > 0 && !paired && (
            <code aria-label="Pairing code" className="mb-2 block text-xl font-semibold tracking-widest select-all">
              {pairing.code}
            </code>
          )}
          <p className="text-sm text-ink-dim">
            {paired
              ? "Paired. Pairing alone does not activate the agent."
              : seconds > 0
                ? `Waiting for your ${connectorName}… Press Refresh connections after pairing.`
                : "Pairing code expired. Create a new code."}
          </p>
          {!paired && (
            <p className="mt-1 text-xs text-ink-faint">
              Expires in {Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, "0")}
            </p>
          )}
          <Button
            className="mt-2"
            size="sm"
            disabled={seconds === 0 || paired === true}
            onClick={() => void copyCode()}
          >
            Copy code
          </Button>
        </div>
      )}
      <div className="mt-5 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">Project connections</h3>
        <Button size="sm" disabled={pending} onClick={() => void refresh()}>
          Refresh connections
        </Button>
      </div>
      {connections?.connections.length === 0 && <p className="mt-2 text-sm text-ink-faint">No saved connections.</p>}
      <ul className="mt-2 space-y-2">
        {connections?.connections.map((item) => (
          <li
            key={item.connectionId}
            className="flex items-start justify-between gap-3 rounded-lg border border-line-soft p-3 text-sm"
          >
            <div className="min-w-0">
              <p className="break-all">{item.owner?.email ?? "Unknown owner"}</p>
              <p className="text-xs text-ink-faint">
                {item.connectionId === currentID
                  ? "Current agent"
                  : item.status === "REMOTE_AGENT_CONNECTION_STATUS_PAIRED"
                    ? "Paired"
                    : "Awaiting pairing"}
              </p>
              {item.binding && (
                <p className="text-xs text-ink-faint">
                  Codex {item.binding.codexVersion} in Orca {item.binding.orcaVersion}
                  {item.binding.hostLabel ? ` on ${item.binding.hostLabel}` : ""}
                </p>
              )}
              <p className="text-xs text-ink-faint">
                Created {readableTime(timestampToISO(item.createdAt))}
                {item.lastSeenAt ? ` · Last seen ${readableTime(timestampToISO(item.lastSeenAt))}` : ""}
              </p>
            </div>
            <Button
              variant="danger"
              aria-label={`Remove connection of ${item.owner?.email ?? "unknown owner"}`}
              size="sm"
              disabled={pending || item.connectionId === currentID}
              onClick={() => void remove(item.connectionId)}
            >
              Remove
            </Button>
          </li>
        ))}
      </ul>
      {currentID && (
        <p className="mt-2 text-xs text-ink-faint">Use the Superverse agent before removing the current connection.</p>
      )}
      {pending && (
        <p className="mt-3 text-sm text-ink-faint" role="status">
          Loading…
        </p>
      )}
      {notice && (
        <p className="mt-3 text-sm text-ink-dim" role="status">
          {notice}
        </p>
      )}
      {error && (
        <p className="mt-3 text-sm text-fail" role="alert">
          {error}
        </p>
      )}
    </Dialog>
  );
}

function message(reason: unknown): string {
  return reason instanceof Error ? reason.message : "The connection request could not be completed.";
}

function readableTime(iso: string | null): string {
  return iso ? new Date(iso).toLocaleString() : "unknown";
}
