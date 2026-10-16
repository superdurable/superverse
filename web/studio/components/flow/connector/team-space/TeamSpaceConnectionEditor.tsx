// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useMemo, useRef, useState } from "react";
import {
  newConnectionView,
  openConnectionForm,
  teamSpaceConnectionView,
  teamSpaceConnectionWriter,
  type ConnectionForm,
  type ConnectorRelease,
  type TeamSpaceConnection,
} from "@/lib/api/flow/team-space-connection";
import { CONNECTOR_REQUEST_FAILED, type ConnectionWriter } from "@/lib/api/flow/connector";
import { errorMessage } from "@/lib/api/transport/api-error";
import { ConnectorForm } from "./ConnectorForm";
import { FIELD_LABEL, NOTE } from "@/components/flow/frame/workspace-styles";

const NAME_INPUT =
  "rounded-md border border-line bg-panel px-2.5 py-1.5 text-[13px] text-ink focus:border-accent focus:shadow-(--focus-ring) focus:outline-none";

/**
 * A Teamspace connection's editor: a new connection of a connector release, or an existing one, with the
 * connector's own form. A key is saved at once; an OAuth method leaves for the provider, which returns the
 * person to `returnPath`. A changed key or sign-in reaches every Project that uses the connection at its next
 * read, and its settings reach a Preview at its next start and Live with its next release.
 */
export function TeamSpaceConnectionEditor({
  teamSpaceId,
  existing,
  release,
  authMethodId = "",
  initialName,
  returnPath,
  onSaved,
  onCancel,
}: {
  teamSpaceId: string;
  /** The connection with its stored credential field names, or null for a new one. */
  existing: { connection: TeamSpaceConnection; storedCredentialFields: string[] } | null;
  release: ConnectorRelease;
  /** The method a Project's connection name declares; a new connection from the catalog chooses one. */
  authMethodId?: string;
  /** A new connection's first name, such as the next free one; the release's name by default. */
  initialName?: string;
  /** Where the provider's OAuth redirect returns, with the outcome in its query. */
  returnPath: () => string;
  onSaved: (connectionId: string) => Promise<void>;
  onCancel: () => void;
}) {
  const [name, setName] = useState(
    existing?.connection.name ?? initialName ?? release.displayName ?? release.connectorId,
  );
  const [opened, setOpened] = useState<{ form: ConnectionForm; writer: ConnectionWriter } | null>(null);
  const [error, setError] = useState("");
  // Read when saving, so the writer, which holds the creation's request ID, outlives edits of them.
  const latest = useRef({ name, returnPath, created: "" });
  useEffect(() => {
    latest.current.name = name;
    latest.current.returnPath = returnPath;
  });
  const connectionId = existing?.connection.connectionId ?? "";
  useEffect(() => {
    const controller = new AbortController();
    openConnectionForm(teamSpaceId, connectionId ? { connectionId } : release, controller.signal).then(
      (form) => {
        if (controller.signal.aborted) return;
        // One writer per form: it holds the creation's request ID, so a retried save is the same connection.
        const writer = teamSpaceConnectionWriter({
          teamSpaceId,
          existing: existing?.connection ?? null,
          release,
          name: () => latest.current.name.trim(),
          sessionNonce: form.sessionNonce ?? "",
          returnPath: () => latest.current.returnPath(),
          onCreated: (id) => (latest.current.created = id),
        });
        setOpened({ form, writer });
      },
      (reason: unknown) => !controller.signal.aborted && setError(errorMessage(reason, CONNECTOR_REQUEST_FAILED)),
    );
    return () => controller.abort();
  }, [teamSpaceId, connectionId, release, existing]);
  const view = useMemo(
    () =>
      existing
        ? teamSpaceConnectionView(existing.connection, existing.storedCredentialFields, release.displayName)
        : newConnectionView(release, name, authMethodId),
    [existing, release, name, authMethodId],
  );
  return (
    <div className="grid gap-3" data-testid="team-space-connection-editor">
      {!existing && (
        <label className="grid gap-1">
          <span className={FIELD_LABEL}>Connection name</span>
          <input
            aria-label="Connection name"
            className={NAME_INPUT}
            maxLength={80}
            onChange={(event) => setName(event.target.value)}
            required
            value={name}
          />
          <small className={NOTE}>Projects of this Teamspace pick the connection by this name.</small>
        </label>
      )}
      {error && (
        <p className="text-xs text-fail" role="alert">
          {error}
        </p>
      )}
      {opened ? (
        <ConnectorForm
          writer={opened.writer}
          connection={view}
          session={opened.form}
          platformClientMethodIds={opened.form.platformClientMethodIds}
          onAuthorized={() => onSaved(latest.current.created || connectionId)}
          onReload={() => onSaved(latest.current.created || connectionId)}
          onError={setError}
          onCancel={onCancel}
        />
      ) : (
        !error && (
          <p className="text-xs text-ink-faint" role="status">
            Loading the connector…
          </p>
        )
      )}
    </div>
  );
}
