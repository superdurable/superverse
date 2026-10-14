// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { closeSync, constants, fstatSync, openSync, readSync } from "node:fs";
import type { UnknownRecord } from "@/lib/shared/json";
import { matching, object, requireCondition } from "./json-assert";

/**
 * Reads an operator's private fixture: a regular file, not a symbolic link,
 * readable only by its owner (0600 or 0400), of 1 to maxBytes bytes. Failures
 * never include the file's contents.
 */
export function readPrivateText(path: string, label: string, maxBytes: number): string {
  let fd: number;
  try {
    fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  } catch {
    throw new Error(`${label} is missing or is not a readable private regular file`);
  }
  try {
    const info = fstatSync(fd);
    if (!info.isFile() || (info.mode & 0o077) !== 0 || info.size < 1 || info.size > maxBytes) {
      throw new Error(`${label} must be a private regular file (0600 or 0400) of at most ${maxBytes} bytes`);
    }
    const bytes = Buffer.alloc(info.size);
    if (readSync(fd, bytes, 0, info.size, 0) !== info.size) throw new Error(`${label} changed while it was read`);
    return bytes.toString("utf8");
  } finally {
    closeSync(fd);
  }
}

/** The JSON value of a private fixture; see readPrivateText. */
export function readPrivateJSON(path: string, label: string, maxBytes: number): unknown {
  const text = readPrivateText(path, label, maxBytes);
  try {
    return JSON.parse(text);
  } catch {
    throw new Error(`${label} must contain JSON`);
  }
}

/** An owner's sign-in credentials; an empty password is a valid account password. */
export interface OwnerCredentials {
  email: string;
  password: string;
}

/** An owner and one of their existing Projects, which addresses its one Session. */
export interface OwnerProject extends OwnerCredentials {
  projectId: string;
}

/** A private JSON fixture holding an owner's credentials; the spec checks its other fields. */
export function ownerFixture(path: string, label: string): UnknownRecord & OwnerCredentials {
  const fixture = object(readPrivateJSON(path, label, 16 * 1024), label);
  requireCondition(
    typeof fixture.email === "string" && fixture.email.includes("@") && typeof fixture.password === "string",
    `${label} requires owner email and password`,
  );
  return fixture as UnknownRecord & OwnerCredentials;
}

/** A private fixture naming an owner and one of their existing READY Projects. */
export function ownerProjectFixture(path: string, label: string): OwnerProject {
  const { email, password, projectId } = ownerFixture(path, label);
  return { email, password, projectId: matching(projectId, /^[a-z0-9]{1,32}$/, `${label} projectId`) };
}
