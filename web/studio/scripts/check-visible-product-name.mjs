#!/usr/bin/env node
// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readdirSync, readFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const failures = [];

/** @param {string} directory */
function scan(directory) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "generated") scan(path);
    } else if (/\.tsx?$/.test(entry.name)) {
      const source = ts.createSourceFile(path, readFileSync(path, "utf8"), ts.ScriptTarget.Latest, true);
      /** @param {ts.Node} node */
      function visit(node) {
        if (
          ts.isStringLiteral(node) ||
          ts.isNoSubstitutionTemplateLiteral(node) ||
          ts.isTemplateHead(node) ||
          ts.isTemplateMiddle(node) ||
          ts.isTemplateTail(node) ||
          ts.isJsxText(node)
        ) {
          const value = node.text;
          // Paths, import specifiers, headers, environment names and persisted protocol framing are identifiers.
          // Inspect prose in literals and JSX, including server error text returned through the BFF.
          const identifier =
            /^(?:[@./][\w@./-]+|https?:\/\/\S+|X-Superverse-[\w-]+|SUPERVERSE_[A-Z_]+|superverse[.:/_-][\w.:/_-]*)$/i.test(
              value,
            ) ||
            /^[a-zA-Z0-9_./-]+\.proto$/.test(value) ||
            value === "‹redacted:superverse›" ||
            value === "superverse" ||
            value === "Superverse message (JSON): ";
          if (/\bsuperverse\b/i.test(value) && !identifier) {
            const position = source.getLineAndCharacterOfPosition(node.getStart(source));
            failures.push(`${relative(root, path)}:${String(position.line + 1)}: internal product name in text`);
          }
        }
        ts.forEachChild(node, visit);
      }
      visit(source);
    }
  }
}

for (const directory of ["app", "components", "lib", "mock"]) scan(join(root, directory));
if (failures.length) {
  console.error(failures.join("\n"));
  process.exitCode = 1;
} else console.log("Studio visible product names: passed");
