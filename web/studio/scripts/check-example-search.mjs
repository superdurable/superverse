#!/usr/bin/env node
// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * The example search's data check (`npm run check:examples`), a static check of generated data, not a unit
 * test. The generator decides every labelled request of app-template/examples/labelled-requests.json in Go
 * and writes each with the Go search's ranking to labelled-search.json. Here the TypeScript search
 * (lib/examples/example-search.ts) must rank every request exactly as Go does, every expected example must
 * be in the grid's top CLOSEST_SHOWN, and every placeholder of the start page must be a labelled request that
 * starts from the default.
 */
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";
// Node resolves no @/ alias; the package's subpath import reaches the search, which imports nothing.
import { CLOSEST_SHOWN, closestPositions, parseExampleSearchIndex } from "#examples/example-search.ts";

const studio = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const generated = join(studio, "lib/generated/example-catalog");
const readJSON = (path) => JSON.parse(readFileSync(path, "utf8"));
const DEFAULT = "default";

const index = parseExampleSearchIndex(readJSON(join(generated, "example-search.json")));
/** @type {{ requests: { text: string, expect: string, ranking: string[] }[] }} */
const labelled = readJSON(join(generated, "labelled-search.json"));
const failures = [];

for (const { text, expect, ranking } of labelled.requests) {
  const ranked = closestPositions(index, text, index.examples.length).map((position) => index.examples[position].id);
  if (ranked.join(",") !== ranking.join(","))
    failures.push(`TypeScript ranks ${ranked.join(", ") || "nothing"}, Go ${ranking.join(", ") || "nothing"}: ${text}`);
  if (expect !== DEFAULT && !ranked.slice(0, CLOSEST_SHOWN).includes(expect))
    failures.push(`${expect} is not in the grid's top ${String(CLOSEST_SHOWN)}: ${text}`);
}

/** The descriptions of the start page's placeholders, as template-catalog.ts declares them. */
function placeholderDescriptions() {
  const path = join(studio, "components/project/templates/template-catalog.ts");
  const source = ts.createSourceFile(path, readFileSync(path, "utf8"), ts.ScriptTarget.Latest, true);
  const descriptions = [];
  const visit = (node) => {
    if (
      ts.isPropertyAssignment(node) &&
      node.name.getText(source) === "description" &&
      ts.isStringLiteral(node.initializer)
    )
      descriptions.push(node.initializer.text);
    ts.forEachChild(node, visit);
  };
  visit(source);
  return descriptions;
}

const placeholders = placeholderDescriptions();
if (placeholders.length === 0) failures.push("template-catalog.ts declares no placeholder description");
for (const description of placeholders)
  if (!labelled.requests.some(({ text, expect }) => text === description && expect === DEFAULT))
    failures.push(`the placeholder is no labelled request that starts from the default: ${description}`);

if (failures.length > 0) {
  for (const failure of failures) console.error(failure);
  console.error("Run make generate-app-template after changing app-template/examples, then check again.");
  process.exit(1);
}
console.log(
  `Example search: ${String(labelled.requests.length)} labelled requests rank alike in Go and TypeScript, every expected example is in the grid's top ${String(CLOSEST_SHOWN)}, and the ${String(placeholders.length)} placeholders start from the default`,
);
