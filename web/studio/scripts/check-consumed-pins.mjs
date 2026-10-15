#!/usr/bin/env node
// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * Studio's consumed-source checks (`npm run check:pins`): the generated initial
 * Design, the boundary that keeps external management UI transports out of
 * Studio, and the integrity of every package pinned outside the npm registry.
 */
import { createHash } from "node:crypto";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const studio = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const failures = [];
const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const readJSON = (path) => JSON.parse(readFileSync(path, "utf8"));

/** Packages the imported UI replaced; neither a dependency nor an import may name them. */
const FORBIDDEN_PACKAGES = [
  "@superdurable/dex-web-v2",
  "@superdurable/flow-definition-renderer",
  "@superdurable/dex-connectors-react",
  "react-router-dom",
];
/** Deleted Dex Web transport modules under lib/; neither the file nor an import of it may return. */
const FORBIDDEN_DEX_WEB_TRANSPORT_FILES = [
  "dex-web-scope.ts",
  "dex-web-scope-cache.ts",
  "hosted-dex-web-proxy.ts",
  "hosted-dex-web-proxy-policy.mjs",
  "project-configuration-proxy.ts",
  "project-configuration-proxy-policy.mjs",
  "connector-oauth-routing.ts",
];
/** Identifiers of the external management UI's capabilities, which Studio does not offer. */
const FORBIDDEN_MANAGEMENT_CAPABILITIES = [
  "entrypointURL",
  "__DEX_WEB_CONFIG__",
  "permissionMode",
  "workerTargetAddress",
  "workQueuePermissions",
];
/** Addresses of a Dex Web server or a remotely loaded Connector runtime. */
const DEX_WEB_ADDRESS =
  /(?:localhost|127\.0\.0\.1):8802|dex-web(?:-[a-z0-9]+)?\.[a-z0-9-]+\.svc|^https?:.*connector.*\.js(?:\?|$)/i;
/** The Flow API server and the Dex routes answer management routes in process, never through an upstream HTTP hop. */
const MANAGEMENT_SERVER_DIRECTORIES = ["lib/server/flow/", "app/dex/"];
/**
 * The only frames Studio renders: the Project's own application, on Preview and on Live, each in its one
 * Interface component, titled and addressed as named here. The Live frame was approved by the owner on
 * 2026-10-14 (the Interface shows Live's page as the prototype does).
 */
const APPLICATION_FRAMES = [
  { member: "components/build/PreviewPanel.tsx", title: "Preview application", source: "preview.url" },
  { member: "components/build/LiveInterface.tsx", title: "Live application", source: "live.url" },
];

const isForbiddenPackage = (name) => FORBIDDEN_PACKAGES.some((pkg) => name === pkg || name.startsWith(`${pkg}/`));
const isRemoteURL = (name) => /^(?:https?:)?\/\//.test(name);
const importsRemovedTransport = (name) =>
  FORBIDDEN_DEX_WEB_TRANSPORT_FILES.some((file) => name.endsWith(file.replace(/\.(?:mjs|ts)$/, "")));

// app-template.json is regenerated with the embedded archive; scripts/check-app-template.sh
// owns exact drift. Here, verify the consumed Design is internally consistent.
function checkInitialDesign() {
  const designs = readdirSync(join(studio, "lib/generated/template-design"));
  if (designs.length !== 1 || designs[0] !== "app-template.json")
    failures.push("Initial Design must be the single generated app-template.json");
  const design = readJSON(join(studio, "lib/generated/template-design/app-template.json"));
  if (!/^app-template@[a-f0-9]{40}$/.test(design.templateRevision) || !design.definitions?.length)
    failures.push("Initial Design has no exact template revision");
  for (const definition of design.definitions ?? []) {
    const { graph } = definition;
    const consistent =
      definition.schemaVersion === "2.0" &&
      definition.valid === true &&
      graph.valid === true &&
      graph.schemaVersion === "2.0" &&
      !graph.diagnostics?.length &&
      graph.flow.name === definition.flowType &&
      definition.digest === `sha256:${sha256(JSON.stringify(graph))}`;
    if (!consistent) failures.push("Initial template FDG digest drift");
  }
}

function checkForbiddenPackages(pkg, lock) {
  for (const name of FORBIDDEN_PACKAGES) {
    if (pkg.dependencies?.[name] || pkg.devDependencies?.[name] || lock.packages?.[`node_modules/${name}`])
      failures.push(`Shared UI dependency remains: ${name}`);
  }
}

function jsxAttribute(node, name, syntax) {
  return node.attributes.properties.find(
    (attribute) => ts.isJsxAttribute(attribute) && attribute.name.getText(syntax) === name,
  )?.initializer;
}

/** Which application frame this is, by its file, title and address, or undefined for any other frame. */
function applicationFrame(member, node, syntax) {
  const title = jsxAttribute(node, "title", syntax);
  const source = jsxAttribute(node, "src", syntax);
  return APPLICATION_FRAMES.find(
    (frame) =>
      member === frame.member &&
      title !== undefined &&
      ts.isStringLiteral(title) &&
      title.text === frame.title &&
      source !== undefined &&
      ts.isJsxExpression(source) &&
      source.expression?.getText(syntax) === frame.source,
  );
}

/** The root layout's inline theme script, which runs before first paint. */
function isThemeBootstrapScript(member, node, syntax) {
  const attributes = node.attributes.properties;
  return (
    member === "app/layout.tsx" &&
    attributes.length === 1 &&
    attributes[0].getText(syntax) === "dangerouslySetInnerHTML={{ __html: THEME_BOOTSTRAP }}"
  );
}

/**
 * Studio renders management UI from its own source: no removed transport,
 * shared or remote UI module, foreign frame or script, cross-frame messaging or
 * upstream management HTTP. Returns the number of scanned source files.
 */
function checkUIBoundary() {
  for (const file of FORBIDDEN_DEX_WEB_TRANSPORT_FILES)
    if (existsSync(join(studio, "lib", file))) failures.push(`Removed Dex Web transport was restored: ${file}`);
  const applicationFrames = new Map(APPLICATION_FRAMES.map((frame) => [frame, 0]));
  let scannedSources = 0;
  const scanFile = (file) => {
    scannedSources++;
    const member = relative(studio, file).split(sep).join("/");
    const syntax = ts.createSourceFile(
      member,
      readFileSync(file, "utf8"),
      ts.ScriptTarget.Latest,
      true,
      member.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );
    const reject = (message) => failures.push(`${message}: ${member}`);
    const walk = (node) => {
      if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) {
        const name = node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier) ? node.moduleSpecifier.text : "";
        if (isForbiddenPackage(name)) reject("Shared UI import remains");
        if (isRemoteURL(name) || importsRemovedTransport(name)) reject("Remote or removed UI transport import");
      }
      if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
        const tag = node.tagName.getText(syntax);
        const frame = tag === "iframe" ? applicationFrame(member, node, syntax) : undefined;
        if (tag === "iframe" && !frame) reject("Management iframe is forbidden");
        else if (frame) applicationFrames.set(frame, applicationFrames.get(frame) + 1);
        if (tag === "script" && !isThemeBootstrapScript(member, node, syntax))
          reject("Remote management JavaScript is forbidden");
      }
      if (ts.isCallExpression(node)) {
        const expression = node.expression.getText(syntax);
        const argument = node.arguments[0];
        const literal = argument && ts.isStringLiteral(argument) ? argument.text : "";
        const loadsModule = expression === "require" || node.expression.kind === ts.SyntaxKind.ImportKeyword;
        if (loadsModule && (isForbiddenPackage(literal) || isRemoteURL(literal)))
          reject("Shared or remote UI module loading is forbidden");
        if (/(?:^|\.)(?:createElement)$/.test(expression) && ["iframe", "script"].includes(literal))
          reject("Dynamic management iframe/script is forbidden");
        if (expression.endsWith(".postMessage")) reject("Cross-frame management transport is forbidden");
        const servesManagement = MANAGEMENT_SERVER_DIRECTORIES.some((directory) => member.startsWith(directory));
        const callsHTTP =
          /(?:^|\.)(?:fetch|request)$/.test(expression) ||
          (/\.(?:get|post|put|patch|delete)$/.test(expression) && /(?:axios|https?|upstream)/i.test(expression));
        if (servesManagement && callsHTTP) reject("Management HTTP upstream transport is forbidden");
      }
      if (ts.isIdentifier(node) && FORBIDDEN_MANAGEMENT_CAPABILITIES.includes(node.text))
        reject("Removed management UI capability remains");
      if (ts.isStringLiteral(node) && DEX_WEB_ADDRESS.test(node.text))
        reject("Dex Web/remote Connector runtime address remains");
      ts.forEachChild(node, walk);
    };
    walk(syntax);
  };
  const scanDirectory = (directory) => {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      const path = join(directory, entry.name);
      if (entry.isDirectory()) {
        if (entry.name !== "generated") scanDirectory(path);
      } else if (/\.(?:[cm]?jsx?|tsx?)$/.test(entry.name)) {
        scanFile(path);
      }
    }
  };
  for (const directory of ["app", "components", "lib", "mock"]) scanDirectory(join(studio, directory));
  for (const [frame, count] of applicationFrames)
    if (count !== 1) failures.push(`The application frame "${frame.title}" must appear exactly once: ${frame.member}`);
  return scannedSources;
}

/** Every package resolved outside the npm registry still downloads with its locked integrity; returns their count. */
async function checkExternalPins(lock) {
  const pins = Object.entries(lock.packages ?? {})
    .filter(
      ([, entry]) =>
        typeof entry.resolved === "string" &&
        /^https:\/\//.test(entry.resolved) &&
        !entry.resolved.includes("registry.npmjs.org"),
    )
    .map(([path, entry]) => ({ path, ...entry }));
  for (const pin of pins) {
    try {
      if (typeof pin.integrity !== "string") throw new Error("missing integrity");
      const [algorithm, expected] = pin.integrity.split("-");
      const response = await fetch(pin.resolved);
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      if (
        createHash(algorithm)
          .update(Buffer.from(await response.arrayBuffer()))
          .digest("base64") !== expected
      )
        throw new Error("release integrity differs");
    } catch (error) {
      failures.push(`${pin.path}: ${error.message}`);
    }
  }
  return pins.length;
}

const lock = readJSON(join(studio, "package-lock.json"));
checkInitialDesign();
checkForbiddenPackages(readJSON(join(studio, "package.json")), lock);
const boundaries = checkUIBoundary();
const pins = await checkExternalPins(lock);
for (const failure of failures) console.error(`[check-consumed-pins] FAIL ${failure}`);
if (failures.length) process.exit(1);
console.log(`[check-consumed-pins] ${boundaries} source boundaries and ${pins} external artifact pins verified`);
