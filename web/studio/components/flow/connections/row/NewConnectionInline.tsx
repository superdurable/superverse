// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { connectorDisplayName, type ConnectionView } from "@/lib/api/flow/connector";
import type { TeamSpaceConnection } from "@/lib/api/flow/team-space-connection";
import { TeamSpaceConnectionEditor } from "@/components/flow/connector/team-space/TeamSpaceConnectionEditor";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import type { SetupSideName } from "@/components/flow/connections/model/row-model";

/** The query parameter that brings an authorization started in a row back to its connection name and side. */
const PICK = "pick";

/** The pick key of a connection name's side, which an authorization's return names. */
const pickKey = (connection: ConnectionView, side: SetupSideName) =>
  `setup:${side}:${connection.connectorId}:${connection.connectionName}`;

const SIDE_NAMES = { preview: "Preview", next: "the next publish" } as const;
/** Whose own a connection tied to one address is: Preview's, or Live's for the next publish. */
const OWNER = { preview: "Preview", next: "Live" } as const;

/**
 * Where a provider's sign-in returns: this page with the row's pick key, so the connection it created is
 * selected for the side that asked.
 */
function useReturnPath(connection: ConnectionView, side: SetupSideName) {
  const pathname = usePathname();
  const search = useSearchParams();
  return () => {
    const query = new URLSearchParams(search.toString());
    query.set(PICK, pickKey(connection, side));
    return `${pathname}?${query}`;
  };
}

/**
 * Takes an authorization's return to a row's side once: a connection it created (OAuth creates it when the
 * sign-in completes) is selected for that side; a failed one says why. The return's query is removed.
 */
export function useAuthorizationReturn(
  connection: ConnectionView,
  side: SetupSideName,
  onReturn: (outcome: { connectionId: string } | { problem: string }) => void,
) {
  const router = useRouter();
  const pathname = usePathname();
  const search = useSearchParams();
  const handled = useRef(false);
  useEffect(() => {
    if (handled.current || search.get(PICK) !== pickKey(connection, side) || !search.get("oauth")) return;
    handled.current = true;
    const outcome = search.get("oauth");
    const id = search.get("connection") ?? "";
    const reason = search.get("reason") ?? "";
    const query = new URLSearchParams(search.toString());
    for (const name of [PICK, "oauth", "connection", "reason"]) query.delete(name);
    router.replace(query.size ? `${pathname}?${query}` : pathname, { scroll: false });
    void Promise.resolve().then(() =>
      onReturn(
        outcome === "success" && id
          ? { connectionId: id }
          : { problem: reason ? `The sign-in did not complete (${reason}).` : "The sign-in did not complete." },
      ),
    );
  });
}

/**
 * A new TeamSpace connection, inline in its row: the TeamSpace's connection form for the connector release
 * the connection name declares. A key is saved at once; OAuth creates the connection when the sign-in
 * completes. Either way it is selected for the side that asked, and any Project of the TeamSpace can pick
 * it. A connector tied to one address says the connection is that side's own, with the side's address.
 */
export function NewConnectionInline({
  teamSpaceId,
  connection,
  side,
  endpointBound,
  address,
  onSaved,
  onCancel,
}: {
  teamSpaceId: string;
  connection: ConnectionView;
  side: SetupSideName;
  /** Why a connection of this connector serves one address; empty when it can serve both. */
  endpointBound: string;
  /** The side's address, for a connection tied to one. */
  address: string;
  onSaved: (connectionId: string) => Promise<void>;
  onCancel: () => void;
}) {
  const returnPath = useReturnPath(connection, side);
  const name = connectorDisplayName(connection);
  return (
    <section
      aria-label={`New ${name} connection for ${SIDE_NAMES[side]}`}
      className="mx-3.5 my-2 grid gap-2.5 rounded-[10px] border-[1.5px] border-accent-rule bg-panel px-3.5 py-3 text-[13px] @min-[780px]/tool:ml-12"
      data-testid="new-connection-inline"
    >
      <div className="flex items-center justify-between gap-2">
        <b className="font-semibold text-ink">
          New {name} connection for {SIDE_NAMES[side]}
        </b>
        <Button variant="quiet" size="sm" aria-label="Cancel the new connection" onClick={onCancel}>
          <Glyph name="close" className="size-3.5" />
        </Button>
      </div>
      {endpointBound && (
        <p className="flex items-start gap-2 rounded-lg border border-line-soft bg-raised px-2.5 py-2 text-[12.5px] text-ink-dim">
          <Glyph name="key" className="mt-0.5 size-3.5 shrink-0" />
          <span>
            {endpointBound} This connection is {OWNER[side]}&apos;s own
            {address ? (
              <>
                , at {OWNER[side]}&apos;s address{" "}
                <code className="font-mono text-[12px] wrap-anywhere text-ink">{address}</code>.
              </>
            ) : (
              "."
            )}
          </span>
        </p>
      )}
      <TeamSpaceConnectionEditor
        teamSpaceId={teamSpaceId}
        existing={null}
        release={{
          connectorId: connection.connectorId,
          modulePath: connection.modulePath ?? "",
          version: connection.moduleVersion ?? "",
          displayName: connection.displayName,
        }}
        authMethodId={connection.authMethodId ?? ""}
        returnPath={returnPath}
        onSaved={onSaved}
        onCancel={onCancel}
      />
      <p className="text-[12px] text-ink-faint">
        Saved to the TeamSpace&apos;s connections, so any project there can pick it.
      </p>
    </section>
  );
}

/** A selected TeamSpace connection's editor, inline in its row's Connection settings. */
export function ConnectionSettingsInline({
  teamSpaceId,
  connection,
  existing,
  side,
  onDone,
}: {
  teamSpaceId: string;
  connection: ConnectionView;
  existing: { connection: TeamSpaceConnection; storedCredentialFields: string[] };
  side: SetupSideName;
  onDone: () => Promise<void>;
}) {
  const returnPath = useReturnPath(connection, side);
  return (
    <div
      className="mx-3.5 my-1.5 rounded-[10px] border border-line-soft bg-panel px-3.5 py-3 @min-[780px]/tool:ml-12"
      data-testid="connection-settings-inline"
    >
      <TeamSpaceConnectionEditor
        teamSpaceId={teamSpaceId}
        existing={existing}
        release={{
          connectorId: connection.connectorId,
          modulePath: connection.modulePath ?? "",
          version: connection.moduleVersion ?? "",
          displayName: connection.displayName,
        }}
        authMethodId={connection.authMethodId ?? ""}
        returnPath={returnPath}
        onSaved={onDone}
        onCancel={() => void onDone()}
      />
    </div>
  );
}
