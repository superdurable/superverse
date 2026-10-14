// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { DraftItem } from "@/lib/api/flow/draft/configuration-draft";
import {
  environmentDraftItem,
  selectionDraftItem,
  selectionPath,
  settingsDraftItem,
  settingsPath,
  writeItem,
  type ConnectorOperation,
  type ConnectorTriggerBinding,
  type EnvironmentValue,
} from "@/lib/api/flow/draft/scope-writes";
import {
  saveApplicationEnvironment,
  type ApplicationEnvironmentSnapshot,
} from "@/lib/api/flow/application-environment";
import type { ConnectionsResponse, ConnectionView } from "@/lib/api/flow/connector";
import type { FlowApiClient } from "@/lib/api/transport/flow-api-client";

/*
 * The writes of Live's next-publish layer: an item is Live's own value, written as on any scope, or
 * "Same as Preview", which removes Live's own value with the same route and `followPreview`, so the item
 * follows what Preview applied again.
 */

/** "Same as Preview" as an item's value: Live's own value goes and the item follows Preview. */
export const FOLLOW_PREVIEW = { followPreview: true } as const;
type FollowPreview = typeof FOLLOW_PREVIEW;

/** Whether an item's value is "Same as Preview". */
export function followsPreview(value: unknown): value is FollowPreview {
  return typeof value === "object" && value !== null && (value as { followPreview?: unknown }).followPreview === true;
}

const sameJson = (a: unknown, b: unknown) => JSON.stringify(a) === JSON.stringify(b);

/**
 * An item of the layer: Live's own value as `item` writes it, or FOLLOW_PREVIEW, which `follow` writes.
 * `follows` is whether the server's item follows Preview now. A refused write compares what the server
 * holds with the value the item resolved to when it was read, so following is no false conflict.
 */
function nextPublishItem<T>(
  item: DraftItem<T>,
  follows: boolean,
  follow: (client: FlowApiClient, revision: string) => Promise<string>,
): DraftItem<T | FollowPreview> {
  const same = (item.same ?? sameJson) as (a: T, b: T) => boolean;
  return {
    key: item.key,
    base: follows ? FOLLOW_PREVIEW : item.base,
    revision: item.revision,
    write: (client, value, revision) =>
      followsPreview(value) ? follow(client, revision) : item.write(client, value, revision),
    read: item.read,
    same: (current, known) => {
      if (followsPreview(current)) return followsPreview(known);
      return same(current, followsPreview(known) ? item.base : known);
    },
  };
}

/** The TeamSpace connection a connection name selects for the next publish, or "Same as Preview". */
export function nextPublishSelectionItem(catalog: ConnectionsResponse, connection: ConnectionView, follows: boolean) {
  return nextPublishItem(selectionDraftItem(catalog, connection), follows, (client, revision) =>
    writeItem(client, catalog, connection, selectionPath(connection), FOLLOW_PREVIEW, revision),
  );
}

/** The settings of a Flow operation or trigger binding for the next publish, or "Same as Preview". */
export function nextPublishSettingsItem(
  catalog: ConnectionsResponse,
  connection: ConnectionView,
  use: ConnectorOperation | ConnectorTriggerBinding,
  base: Record<string, unknown>,
  follows: boolean,
) {
  return nextPublishItem(settingsDraftItem(catalog, connection, use, base), follows, (client, revision) =>
    writeItem(client, catalog, connection, settingsPath(connection, use), FOLLOW_PREVIEW, revision),
  );
}

/** An environment setting for the next publish, or "Same as Preview"; a secret never follows. */
export function nextPublishEnvironmentItem(
  snapshot: ApplicationEnvironmentSnapshot,
  definitionRevision: string,
  name: string,
  secret: boolean,
  onWritten: (snapshot: ApplicationEnvironmentSnapshot) => void,
  follows: boolean,
): DraftItem<EnvironmentValue | FollowPreview> {
  return nextPublishItem(
    environmentDraftItem(snapshot, definitionRevision, name, secret, onWritten),
    follows,
    async (client, revision) => {
      const written = await saveApplicationEnvironment(
        client,
        { ...snapshot, configurationRevision: Number(revision) },
        definitionRevision,
        { values: {}, secrets: {}, remove: [], followPreview: [name] },
      );
      onWritten(written);
      return String(written.configurationRevision);
    },
  );
}
