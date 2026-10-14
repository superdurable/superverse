// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * Keeps the generated summary block of every Studio directory README current (`npm run check:readmes`),
 * modeled on scripts/check-package-readmes.py. The block follows the README title. Its Purpose sentence
 * is hand-written; Importers, Imports (with the remote APIs the directory calls) and the mermaid chart
 * derive from the module graph. web/studio/README.md lists every Purpose in its module table.
 * --write regenerates every block and the table, keeps each Purpose and creates a missing README.
 */
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { isConfigFile, listedFiles, loadModuleGraph, studio } from "./module-graph.mjs";

const WORD_LIMIT = 30;
const BEGIN = "<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->";
const END = "<!-- END GENERATED DIRECTORY SUMMARY -->";
const TABLE_BEGIN = "<!-- BEGIN GENERATED MODULE TABLE (scripts/check-readmes.mjs; edit the README Purpose) -->";
const TABLE_END = "<!-- END GENERATED MODULE TABLE -->";
const PROTO = resolve(studio, "../../api/superverse/v2/superverse.proto");

/**
 * Remote APIs a directory reaches that no RPC call literal shows. A dispatcher may pass a computed method
 * name to its RPC calls; everywhere else the method must be a literal.
 */
const REACHES = {
  "lib/api/flow": { apis: ["Flow API"] },
  "lib/api/transport": { dispatcher: true, apis: ["Studio BFF API", "Flow API", "Preview application API"] },
  "lib/server/flow": {
    dispatcher: true,
    apis: [
      "`FlowRunService` API",
      "`FlowDiagnosticsService` API",
      "`ConnectorService` API",
      "`ApplicationEnvironmentService` API",
    ],
  },
  "lib/server/rpc": { dispatcher: true, apis: ["Superverse business gRPC API"] },
  "tests/e2e/integration": { dispatcher: true, apis: ["`PreviewService` API"] },
  "tests/e2e/support": { apis: ["Studio BFF API", "GitHub REST API", "Local Git host JSON API"] },
};

const write = process.argv.includes("--write");
const { files, problems } = loadModuleGraph();

/** The service of every RPC; method names are unique across services. */
function rpcServices() {
  const services = new Map();
  let service = null;
  for (const line of readFileSync(PROTO, "utf8").split("\n")) {
    service = line.match(/^service (\w+) \{/)?.[1] ?? (line.startsWith("}") ? null : service);
    const method = service && line.match(/^\s+rpc (\w+)\(/)?.[1];
    if (method) services.set(method, service);
  }
  return services;
}

/** Counts words; a backticked path is one word. */
const words = (sentence) =>
  sentence
    .replace(/`[^`]*`/g, "path")
    .split(/\s+/)
    .filter(Boolean).length;
const series = (items) => (items.length === 1 ? items[0] : `${items.slice(0, -1).join(", ")} and ${items.at(-1)}`);

/**
 * The remote APIs one file calls: its directory's REACHES entry and the service of every RPC call literal.
 * Reports an RPC call that names no proto method, and a computed method outside a REACHES dispatcher.
 */
function remoteApis(file, services) {
  const reaches = REACHES[file.module];
  const apis = [...(reaches?.apis ?? [])];
  for (const { method, line } of file.rpcCalls) {
    if (method === null) {
      if (!reaches?.dispatcher)
        problems.push(
          `${file.path}:${line}: an RPC call names its method with a literal, or REACHES lists a dispatcher`,
        );
    } else if (services.has(method)) apis.push(`\`${services.get(method)}\` API`);
    else problems.push(`${file.path}:${line}: ${method} is not an RPC of ${relative(studio, PROTO)}`);
  }
  return apis;
}

/**
 * Collects the importers, imports and remote APIs of every directory with source files; root config
 * files are not modules. Reports REACHES entries of directories without source files.
 */
function moduleEdges() {
  const modules = new Map();
  for (const file of files.values())
    if (!isConfigFile(file.path) && !modules.has(file.module))
      modules.set(file.module, { importers: new Set(), imports: new Set(), apis: new Set() });
  const services = rpcServices();
  for (const file of files.values()) {
    const owner = modules.get(file.module);
    if (!owner) continue;
    for (const { module: target } of file.imports) {
      if (!target || target === file.module) continue;
      owner.imports.add(target);
      if (!file.module.startsWith("tests/")) modules.get(target)?.importers.add(file.module);
    }
    for (const api of remoteApis(file, services)) owner.apis.add(api);
  }
  for (const directory of Object.keys(REACHES))
    if (!modules.has(directory))
      problems.push(`scripts/check-readmes.mjs: REACHES lists ${directory}, which has no source files`);
  return modules;
}

/** Groups directories by their first `depth` segments (one for app, mock and scripts); a group of one keeps its path. */
function collapsed(items, depth) {
  const groups = new Map();
  for (const [path] of items) {
    const parts = path.split("/");
    const key = parts.slice(0, ["app", "mock", "scripts"].includes(parts[0]) ? 1 : depth).join("/");
    groups.set(key, [...(groups.get(key) ?? []), path]);
  }
  return [...groups]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, members]) => (members.length === 1 ? [members[0], null] : [`${key}/**`, members.length]));
}

const code = ([path, count]) => (count === null ? `\`${path}\`` : `\`${path}\` (${count})`);
const label = ([path, count]) => (count === null ? path : `${path} (${count})`);

function importersSentence(importers) {
  if (!importers.length) return "no production directory imports this directory.";
  const single = importers.length === 1 && importers[0][1] === null;
  return `${series(importers.map(code))} ${single ? "imports" : "import"} this directory.`;
}

function importsSentence(imports, apis) {
  const clauses = [];
  if (imports.length) clauses.push(`imports ${series(imports.map(code))}`);
  if (apis.length) {
    const stems = apis.map((name) => name.replace(/ API$/, ""));
    clauses.push(`calls the ${series(stems)} ${stems.length === 1 ? "API" : "APIs"}`);
  }
  return clauses.length
    ? `it ${clauses.join(" and ")}.`
    : "it imports no other Studio directory and calls no remote API.";
}

/**
 * Returns items as they are, else collapsed by each of `depths` in turn, whichever first renders within the
 * limit; the last candidate when none does.
 */
function fit(items, render, depths) {
  const candidates = [items, ...depths.map((depth) => collapsed(items, depth))];
  return candidates.find((candidate) => words(render(candidate)) <= WORD_LIMIT) ?? candidates.at(-1);
}

/** The generated summary block of one directory README. */
function summary(directory, owner, purpose) {
  const apis = [...owner.apis].sort((a, b) => a.localeCompare(b, "en", { sensitivity: "base" }));
  // Fan-in never forces a split, so importers may collapse to one segment; imports collapse only to two.
  const importers = fit(
    [...owner.importers].sort().map((path) => [path, null]),
    importersSentence,
    [2, 1],
  );
  const imports = fit(
    [...owner.imports].sort().map((path) => [path, null]),
    (items) => importsSentence(items, apis),
    [2],
  );
  const importsText = importsSentence(imports, apis);
  if (words(importsText) > WORD_LIMIT)
    problems.push(
      `${directory}: the Imports sentence has ${words(importsText)} words even when collapsed, limit ${WORD_LIMIT}; the directory does too much, split it`,
    );
  return [
    BEGIN,
    `Purpose: ${purpose}`,
    "",
    `Importers: ${importersSentence(importers)}`,
    "",
    `Imports: ${importsText}`,
    "",
    "```mermaid",
    "flowchart LR",
    `  self["${directory}"]`,
    ...importers.map((item, index) => `  c${index}["${label(item)}"] --> self`),
    ...imports.map((item, index) => `  self --> d${index}["${label(item)}"]`),
    ...apis.map((name, index) => `  self --> e${index}(["${name.replaceAll("`", "")}"])`),
    "```",
    END,
  ].join("\n");
}

const escape = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
const blocks = (text, begin = BEGIN, end = END) =>
  text.match(new RegExp(`${escape(begin)}[\\s\\S]*?${escape(end)}`, "g")) ?? [];
const purposeOf = (text) =>
  blocks(text)[0]
    ?.match(/^Purpose: (.*)$/m)?.[1]
    .trim() ?? "";

function purposeProblem(purpose) {
  if (!purpose || purpose.includes("TODO")) return "write the Purpose sentence";
  const plain = purpose.replace(/`[^`]*`/g, "path");
  if (!plain.endsWith(".") || /[.!?]\s/.test(plain)) return "Purpose must be exactly one sentence ending with a period";
  if (words(purpose) > WORD_LIMIT) return `Purpose has ${words(purpose)} words, limit ${WORD_LIMIT}`;
  return null;
}

/** Returns text with block right after its title and every earlier block removed. */
function regenerate(text, block) {
  const [title, ...lines] = text.split("\n");
  let rest = lines.join("\n");
  for (const old of blocks(text)) rest = rest.replace(old, "");
  rest = rest.replace(/^\n+|\n+$/g, "");
  return `${title}\n\n${block}\n${rest ? `\n${rest}\n` : ""}`;
}

function readmeProblems(name, text, expected) {
  const [title, ...lines] = text.split("\n");
  const found = blocks(text);
  const result = [];
  if (!title.startsWith("# ")) result.push(`${name}: must start with a \`# Title\` line`);
  if (found.length !== 1) result.push(`${name}: needs exactly one generated summary block, found ${found.length}`);
  else {
    if (!lines.join("\n").replace(/^\n+/, "").startsWith(BEGIN))
      result.push(`${name}: the generated summary block must follow the title`);
    if (found[0] !== expected) result.push(`${name}: the generated summary block is stale`);
  }
  const problem = purposeProblem(purposeOf(text));
  if (problem) result.push(`${name}: ${problem}`);
  return result;
}

function moduleTable(purposes) {
  const rows = purposes.map(([directory, purpose]) => `| [\`${directory}\`](${directory}/README.md) | ${purpose} |`);
  return [TABLE_BEGIN, "", "| Directory | Purpose |", "| --- | --- |", ...rows, "", TABLE_END].join("\n");
}

/** Relative Markdown links of every Studio README resolve to a file or directory. */
function linkProblems() {
  return listedFiles()
    .filter((path) => path.endsWith("README.md"))
    .flatMap((readme) =>
      [...readFileSync(join(studio, readme), "utf8").matchAll(/\]\(([^)\s]+)\)/g)]
        .map(([, target]) => target)
        .filter((target) => !/^(?:[a-z]+:|#)/.test(target))
        .filter((target) => !existsSync(resolve(studio, dirname(readme), decodeURI(target.split("#")[0]))))
        .map((target) => `${readme}: link ${target} does not resolve`),
    );
}

// Every directory with source files has a README; --write creates a missing one.
const purposes = [];
for (const [directory, owner] of [...moduleEdges()].sort(([a], [b]) => a.localeCompare(b))) {
  const name = `${directory}/README.md`;
  const path = join(studio, name);
  const existing = existsSync(path) ? readFileSync(path, "utf8") : null;
  const expected = summary(directory, owner, purposeOf(existing ?? "") || "TODO");
  if (existing === null && !write) {
    problems.push(`${name}: missing`);
    continue;
  }
  let text = existing ?? `# ${directory}\n`;
  if (write && text.startsWith("# ")) {
    text = regenerate(text, expected);
    writeFileSync(path, text);
  }
  problems.push(...readmeProblems(name, text, expected));
  purposes.push([directory, purposeOf(text)]);
}
const overview = join(studio, "README.md");
let overviewText = readFileSync(overview, "utf8");
const table = moduleTable(purposes);
if (write) {
  for (const old of blocks(overviewText, TABLE_BEGIN, TABLE_END)) overviewText = overviewText.replace(old, table);
  writeFileSync(overview, overviewText);
}
const tables = blocks(overviewText, TABLE_BEGIN, TABLE_END);
if (tables.length !== 1) problems.push(`README.md: needs exactly one generated module table, found ${tables.length}`);
else if (tables[0] !== table) problems.push("README.md: the generated module table is stale");
problems.push(...linkProblems());
if (problems.length) {
  console.error([...problems, "run `make generate-studio-readmes`, then write each Purpose sentence"].join("\n"));
  process.exit(1);
}
console.log(`Studio READMEs: ${purposes.length} generated summaries and the module table are current`);
