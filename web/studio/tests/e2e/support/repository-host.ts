// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { execFile } from "node:child_process";
import { promisify } from "node:util";
import type { UnknownRecord } from "@/lib/shared/json";
import { matching, object, requireCondition, string } from "./json-assert";
import { readPrivateText } from "./private-files";

// Reads of the Git host that the deployed server uses, for the commits the Agent
// pushed. SUPERVERSE_E2E_REPOSITORY_HOST names the host, which the runner sets from the
// deployed profile: "github" through the operator's authenticated gh CLI (the final
// acceptance and the hosted environments), or "localgit" through the read-only JSON
// interface of the local Git host (the local lanes). A failure never repeats gh output
// or a response, which may describe private repositories.

type RepositoryHost = "github" | "localgit";

function repositoryHost(): RepositoryHost {
  const host = process.env.SUPERVERSE_E2E_REPOSITORY_HOST ?? "github";
  requireCondition(
    host === "github" || host === "localgit",
    "SUPERVERSE_E2E_REPOSITORY_HOST must be github or localgit",
  );
  return host;
}

const execFileAsync = promisify(execFile);
const GH_OPTIONS = { maxBuffer: 8 * 1024 * 1024, timeout: 120_000 };

async function githubAPI(args: string[]): Promise<string> {
  return (await execFileAsync("gh", ["api", ...args], GH_OPTIONS)).stdout;
}

/** One GitHub API object; fields make the call a POST, booleans as typed (-F) fields. GitHub deployments only. */
export async function githubJSON(path: string, fields?: Record<string, string | boolean>): Promise<UnknownRecord> {
  requireCondition(
    repositoryHost() === "github",
    "This read needs the real GitHub API, which the local Git host does not provide",
  );
  const args = [path];
  if (fields) {
    args.push("--method", "POST");
    for (const [key, value] of Object.entries(fields))
      args.push(typeof value === "boolean" ? "-F" : "-f", `${key}=${value}`);
  }
  try {
    return object(JSON.parse(await githubAPI(args)), "GitHub response");
  } catch {
    throw new Error("The real GitHub API did not return a confirmed result");
  }
}

/** One read of the local Git host's JSON interface, with the administrator token of the runner's private file. */
async function localGitJSON(path: string): Promise<UnknownRecord> {
  const origin = process.env.SUPERVERSE_E2E_LOCAL_GIT_URL;
  const tokenFile = process.env.SUPERVERSE_E2E_LOCAL_GIT_TOKEN_FILE;
  requireCondition(
    origin && tokenFile,
    "The local Git host needs SUPERVERSE_E2E_LOCAL_GIT_URL and SUPERVERSE_E2E_LOCAL_GIT_TOKEN_FILE",
  );
  const token = readPrivateText(tokenFile, "Local Git administrator token", 4096).trim();
  try {
    const response = await fetch(`${origin}/_api/v1/${path}`, {
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(30_000),
    });
    requireCondition(response.ok, "The local Git host refused the read");
    return object(await response.json(), "Local Git host response");
  } catch {
    throw new Error("The local Git host did not return a confirmed result");
  }
}

/** A file's text at an exact ref. */
export async function repositoryFile(repository: string, path: string, ref: string): Promise<string> {
  try {
    const content =
      repositoryHost() === "github"
        ? await githubAPI([`repos/${repository}/contents/${path}?ref=${ref}`, "--jq", ".content"])
        : string(
            (
              await localGitJSON(
                `repositories/${repository}/file?ref=${encodeURIComponent(ref)}&path=${encodeURIComponent(path)}`,
              )
            ).content,
            "Local Git file content",
          );
    return Buffer.from(content.replace(/\s/g, ""), "base64").toString("utf8");
  } catch {
    throw new Error("Cannot read the exact repository source through the repository host");
  }
}

/** The commit a branch points at now. */
export async function branchHead(repository: string, branch: string): Promise<string> {
  let head: string;
  try {
    head =
      repositoryHost() === "github"
        ? (
            await githubAPI([`repos/${repository}/git/ref/heads/${encodeURIComponent(branch)}`, "--jq", ".object.sha"])
          ).trim()
        : string(
            (await localGitJSON(`repositories/${repository}/branch?name=${encodeURIComponent(branch)}`)).sha,
            "Local Git branch commit",
          );
  } catch {
    throw new Error("Cannot resolve the repository branch through the repository host");
  }
  return matching(head, /^[0-9a-f]{40}$/, "Repository branch commit identity");
}
