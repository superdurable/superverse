// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { arrayField, asRecord, isNonEmptyString, isRecord } from "@/lib/shared/json";

interface SlackChannel {
  id: string;
  name: string;
  isPrivate: boolean;
  /** Whether the app is in the channel; it receives a channel's events only once invited. */
  isMember: boolean;
}
interface SlackUser {
  id: string;
  displayName: string;
}

export interface SlackChannelPage {
  channels: SlackChannel[];
  nextCursor: string;
}
export interface SlackUserPage {
  users: SlackUser[];
  nextCursor: string;
}

export function parseSlackChannelPage(value: Record<string, unknown>): SlackChannelPage {
  assertSlackOK(value);
  const channels = arrayField(value.channels).flatMap((item) => {
    if (!isRecord(item) || !isNonEmptyString(item.id) || !isNonEmptyString(item.name) || item.is_archived === true)
      return [];
    return [{ id: item.id, name: item.name, isPrivate: item.is_private === true, isMember: item.is_member === true }];
  });
  return { channels, nextCursor: nextCursor(value) };
}

export function parseSlackUserPage(value: Record<string, unknown>): SlackUserPage {
  assertSlackOK(value);
  const users = arrayField(value.members).flatMap((item) => {
    if (
      !isRecord(item) ||
      !isNonEmptyString(item.id) ||
      item.deleted === true ||
      item.is_bot === true ||
      item.id === "USLACKBOT"
    )
      return [];
    const profile = asRecord(item.profile);
    const displayName = firstText(profile.display_name, profile.real_name, item.name, item.id);
    return [{ id: item.id, displayName }];
  });
  return { users, nextCursor: nextCursor(value) };
}

function assertSlackOK(value: Record<string, unknown>) {
  if (value.ok !== true) throw new Error(isNonEmptyString(value.error) ? value.error : "Slack rejected the request");
}

function nextCursor(value: Record<string, unknown>): string {
  const metadata = asRecord(value.response_metadata);
  return isNonEmptyString(metadata.next_cursor) ? metadata.next_cursor.trim() : "";
}

function firstText(...values: unknown[]): string {
  return values.find(isNonEmptyString) as string;
}
