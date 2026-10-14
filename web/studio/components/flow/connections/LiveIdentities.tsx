// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { connectionKey, connectorDisplayName } from "@/lib/api/flow/connector";
import { useFlowApiScope } from "@/lib/flow/FlowApiProvider";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { cx } from "@/components/ui/cx";
import { connectionWords, triggerWords } from "@/components/flow/connections/model/connection-words";
import { channelOf } from "@/components/flow/connections/model/setting-defaults";
import {
  useChannelNames,
  useConnectionSessions,
  useReleaseManifests,
  useScopeCatalog,
} from "@/components/flow/connections/model/use-scope-catalog";

/**
 * Who Live will act as and where it listens once this draft is published: each connection name with
 * the TeamSpace connection it selects and that connection's state, and each trigger with its channel,
 * named as Slack lists it or by its ID when the list is unavailable. Runs inside the draft's
 * FlowApiProvider; `version` changes whenever the draft does.
 */
export function LiveIdentities({ version }: { version: string }) {
  const scope = useFlowApiScope();
  const { catalog, error } = useScopeCatalog(scope, version);
  const sessions = useConnectionSessions(scope, catalog);
  const manifests = useReleaseManifests(catalog, useFlowHost().teamSpaceId ?? "");
  const channels = useChannelNames(catalog, sessions);
  if (!catalog)
    return (
      <p className={cx("text-[12.5px]", error ? "text-fail" : "text-ink-faint")} role={error ? "alert" : "status"}>
        {error || "Reading the connections…"}
      </p>
    );
  if (catalog.connections.length === 0)
    return <p className="text-[12.5px] text-ink-faint">This version uses no connection.</p>;
  return (
    <ul className="grid gap-2 text-[12.5px]" data-testid="live-identities">
      {catalog.connections.map((connection) => {
        const key = connectionKey(connection);
        const opened = sessions.get(key);
        const manifest =
          opened && "session" in opened ? opened.session.manifest : (manifests.get(connection.connectorId) ?? null);
        const state = connectionWords(connection);
        return (
          <li key={key} className="grid gap-0.5">
            <span className="text-ink">
              <b className="font-semibold">{connection.connectionName}</b>
              <span className="text-ink-faint"> · {connectorDisplayName(connection)}</span>
              {" → "}
              {connection.selectedConnection ? (
                <>
                  TeamSpace connection <b className="font-semibold">{connection.selectedConnection.name}</b>
                  <span className="text-ink-faint"> ({state.words})</span>
                </>
              ) : (
                <span className="text-fail">no TeamSpace connection selected</span>
              )}
            </span>
            {(connection.triggerUses ?? []).map((use) => {
              const channel = channelOf(use);
              return (
                <span key={`${use.triggerName}/${use.bindingName}`} className="pl-3 text-ink-dim">
                  {triggerWords(manifest, use.triggerName)}
                  {channel ? (
                    <>
                      {" in "}
                      <b className="font-semibold text-ink">{channels.get(key)?.get(channel) ?? channel}</b>
                    </>
                  ) : (
                    " · no channel"
                  )}
                </span>
              );
            })}
          </li>
        );
      })}
    </ul>
  );
}
