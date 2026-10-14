// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const repository = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const checkedIn = path.join(repository, "web/studio/lib/generated/protobuf");
const checking = process.argv.includes("--check");
const output = checking ? mkdtempSync(path.join(tmpdir(), "sv2-protobuf-types-")) : checkedIn;
const generator = require.resolve("@grpc/proto-loader/build/bin/proto-loader-gen-types.js");

function files(directory, prefix = "") {
  return readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) => {
      const relative = path.join(prefix, entry.name);
      return entry.isDirectory() ? files(path.join(directory, entry.name), relative) : [relative];
    })
    .sort();
}

try {
  if (!checking) rmSync(checkedIn, { recursive: true, force: true });
  const result = spawnSync(
    process.execPath,
    [
      generator,
      `--outDir=${output}`,
      "--grpcLib=@grpc/grpc-js",
      "--longs=String",
      "--enums=String",
      "--defaults",
      "--oneofs",
      "superverse/v2/superverse.proto",
      "google/rpc/status.proto",
      "google/rpc/error_details.proto",
      "--includeDirs",
      "api",
      "web/studio/proto",
    ],
    { cwd: repository, stdio: "inherit" },
  );
  if (result.status !== 0) throw new Error("Protobuf TypeScript generation failed");
  // grpc-js interface generation emits whitespace-only separator lines. Normalize
  // them in both generation and drift checking, never by editing generated files.
  for (const file of files(output)) {
    const outputFile = path.join(output, file);
    writeFileSync(
      outputFile,
      readFileSync(outputFile, "utf8")
        .replace(/[ \t]+$/gm, "")
        .trimEnd() + "\n",
    );
  }
  if (checking) {
    const generated = files(output);
    if (JSON.stringify(generated) !== JSON.stringify(files(checkedIn)))
      throw new Error("Protobuf TypeScript file inventory differs");
    for (const file of generated) {
      if (!readFileSync(path.join(output, file)).equals(readFileSync(path.join(checkedIn, file))))
        throw new Error(`Protobuf TypeScript differs: ${file}`);
    }
  }
} finally {
  if (checking) rmSync(output, { recursive: true, force: true });
}
