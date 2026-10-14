// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * Studio's source rules (`npm run check:modules`, decision 0012): directory and file limits, names, no
 * JavaScript outside scripts/, an acyclic directory import graph and client reachability. Client
 * reachability replaces `server-only` markers: no module a 'use client' file reaches may be server code.
 */
import { dirname } from "node:path";
import { isConfigFile, isScript, loadModuleGraph } from "./module-graph.mjs";

const FILE_LIMIT = 10;
const DIRECTORY_LIMIT = 10;
const LINE_LIMIT = 400;

const KEBAB = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const PASCAL = /^[A-Z][A-Za-z0-9]*$/;
const GENERIC_FILES = new Set(
  "index utils util helpers helper common misc types constants http handler routes copy payload reducer api".split(" "),
);
const GENERIC_DIRECTORIES = new Set("common utils util helpers helper misc foundation".split(" "));
/** Next.js names these app/ files; their names are not ours to choose. */
const NEXT_FILES = /^(?:page|layout|template|loading|error|global-error|not-found|default|route)\.(?:ts|tsx)$/;
/** Server code a client bundle must never contain. */
const SERVER_DIRECTORIES = ["lib/server/", "mock/backend/", "mock/store/"];
const SERVER_PACKAGES = /^(?:node:|@grpc\/)/;

const { files, problems } = loadModuleGraph();

/** Route segments under app/ are Next.js URL structure; the module rules cover only app/'s own files. */
const isRouteSegment = (directory) => directory.startsWith("app/");
const directoryOf = (path) => (dirname(path) === "." ? "" : dirname(path));
const ancestors = (directory) => directory.split("/").map((_, index, parts) => parts.slice(0, index + 1).join("/"));

function checkLimits() {
  const sourceFiles = new Map();
  const children = new Map();
  for (const path of files.keys()) {
    const directory = directoryOf(path);
    if (isRouteSegment(directory)) continue;
    sourceFiles.set(directory, (sourceFiles.get(directory) ?? 0) + 1);
    if (!directory) continue;
    for (const ancestor of ancestors(directory)) {
      const parent = directoryOf(ancestor);
      if (!children.has(parent)) children.set(parent, new Set());
      children.get(parent).add(ancestor);
    }
  }
  for (const [directory, count] of sourceFiles)
    if (count > FILE_LIMIT)
      problems.push(`${directory || "web/studio"}: ${count} source files, limit ${FILE_LIMIT}; split it`);
  for (const [directory, members] of children)
    if (members.size > DIRECTORY_LIMIT)
      problems.push(
        `${directory || "web/studio"}: ${members.size} sub-directories with source, limit ${DIRECTORY_LIMIT}`,
      );
}

function checkLength() {
  for (const { path, lines } of files.values())
    if (lines > LINE_LIMIT)
      problems.push(`${path}: ${lines} lines, limit ${LINE_LIMIT}; split it along a responsibility seam`);
}

function checkNames() {
  for (const { path, text, valueExports } of files.values()) {
    const directory = directoryOf(path);
    const name = path.slice(directory ? directory.length + 1 : 0);
    if (path.endsWith(".css") || isConfigFile(path) || (path.startsWith("app/") && NEXT_FILES.test(name))) continue;
    const [stem, extension] = [name.replace(/\.[^.]+$/, ""), name.slice(name.lastIndexOf("."))];
    const base = stem.replace(/\.spec$/, "");
    if (stem.endsWith(".spec") && !(path.startsWith("tests/e2e/") && name.endsWith(".spec.ts")))
      problems.push(`${path}: *.spec.ts files live only under tests/e2e`);
    if (extension === ".tsx" && !PASCAL.test(base)) problems.push(`${path}: .tsx files are PascalCase`);
    if ((extension === ".ts" || extension === ".mjs") && !KEBAB.test(base))
      problems.push(`${path}: ${extension} files are kebab-case`);
    if (GENERIC_FILES.has(base) && path !== "components/agent/index.ts")
      problems.push(`${path}: name the file for its main export, not "${base}"`);
    if (extension === ".ts") {
      const hooks = valueExports.length > 0 && valueExports.every((value) => /^use[A-Z]/.test(value));
      if (base.startsWith("use-") !== hooks)
        problems.push(`${path}: a .ts file is named use-*.ts exactly when every value export is a hook`);
    }
    if (/\bcreateContext\s*[<(]/.test(text) && !/Provider\.tsx$/.test(name))
      problems.push(`${path}: a context lives with its provider and hooks in an XxxProvider.tsx`);
  }
  const directories = new Set(
    [...files.keys()].map(directoryOf).flatMap((directory) => (directory ? ancestors(directory) : [])),
  );
  for (const directory of directories) {
    if (isRouteSegment(directory)) continue;
    const name = directory.slice(directory.lastIndexOf("/") + 1);
    if (!KEBAB.test(name)) problems.push(`${directory}: directory names are kebab-case`);
    if (GENERIC_DIRECTORIES.has(name)) problems.push(`${directory}: name the directory for its responsibility`);
  }
}

function checkNoJavaScript() {
  for (const path of files.keys())
    if (isScript(path) && !path.startsWith("scripts/") && !isConfigFile(path))
      problems.push(`${path}: JavaScript lives only in scripts/ and the root config files; write TypeScript`);
}

/** Tarjan's strongly connected components of the directory graph; every component of two or more is a cycle. */
function checkAcyclic() {
  const edges = new Map();
  for (const file of files.values())
    for (const { path, module: target } of file.imports) {
      if (target === undefined || target === file.module) continue;
      if (!edges.has(file.module)) edges.set(file.module, new Map());
      if (!edges.get(file.module).has(target)) edges.get(file.module).set(target, `${file.path} -> ${path}`);
    }
  const index = new Map();
  const low = new Map();
  const stack = [];
  let counter = 0;
  const connect = (node) => {
    index.set(node, counter);
    low.set(node, counter++);
    stack.push(node);
    for (const next of edges.get(node)?.keys() ?? []) {
      if (!index.has(next)) {
        connect(next);
        low.set(node, Math.min(low.get(node), low.get(next)));
      } else if (stack.includes(next)) low.set(node, Math.min(low.get(node), index.get(next)));
    }
    if (low.get(node) !== index.get(node)) return;
    const component = [];
    let member;
    do {
      member = stack.pop();
      component.push(member);
    } while (member !== node);
    if (component.length < 2) return;
    const witnesses = component.flatMap((from) =>
      [...(edges.get(from) ?? [])].filter(([to]) => component.includes(to)).map(([, witness]) => witness),
    );
    problems.push(`directory import cycle among ${component.sort().join(", ")}:\n    ${witnesses.join("\n    ")}`);
  };
  for (const node of new Set([...files.values()].map((file) => file.module))) if (!index.has(node)) connect(node);
}

function checkClientReachability() {
  const reachedFrom = new Map();
  const queue = [];
  for (const file of files.values())
    if (file.directives.includes("use client")) {
      reachedFrom.set(file.path, file.path);
      queue.push(file.path);
    }
  while (queue.length) {
    const file = files.get(queue.shift());
    const entry = reachedFrom.get(file.path);
    if (SERVER_DIRECTORIES.some((directory) => file.path.startsWith(directory)))
      problems.push(`${file.path}: server code reachable from the client module ${entry}`);
    for (const target of file.imports) {
      if (target.typeOnly) continue;
      if (target.package && SERVER_PACKAGES.test(target.package))
        problems.push(`${file.path}: imports ${target.specifier}, reachable from the client module ${entry}`);
      const next = target.path && files.get(target.path);
      if (!next || reachedFrom.has(next.path) || next.directives.includes("use server")) continue;
      reachedFrom.set(next.path, entry);
      queue.push(next.path);
    }
  }
}

checkLimits();
checkLength();
checkNames();
checkNoJavaScript();
checkAcyclic();
checkClientReachability();
if (problems.length) {
  console.error([...new Set(problems)].join("\n"));
  process.exit(1);
}
console.log(`Studio modules: ${files.size} source files within the limits, names, layering and client reachability`);
