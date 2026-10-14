// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * The import graph of Studio's hand-written source, shared by check-modules.mjs and check-readmes.mjs.
 * Files come from git; TypeScript parses them and resolves every import through tsconfig.json (`@/`,
 * JSON and CSS by path). An unresolved import or an unclassified npm package is a problem, never a
 * silently missing edge, so the checks built on this graph fail closed.
 */
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

export const studio = resolve(dirname(fileURLToPath(import.meta.url)), "..");

/** Every npm package Studio imports, reviewed by hand. An unlisted package fails, and so does an unused entry. */
const EXTERNAL = {
  react: "UI rendering",
  "react-dom": "rendering into the page outside a component's own box",
  next: "the application framework",
  "react-markdown": "Markdown rendering",
  "remark-gfm": "GitHub-flavored Markdown",
  unist: "Markdown syntax tree types",
  "@xyflow/react": "the Flow canvas",
  dagre: "graph layout",
  elkjs: "the Flow canvas layout and line routing",
  "@zxing/browser": "QR code scanning",
  "@grpc/grpc-js": "the gRPC client",
  "@grpc/proto-loader": "protobuf descriptors",
  "@playwright/test": "end-to-end tests",
  "@jridgewell/trace-mapping": "source map lookups of the E2E spec coverage",
  "playwright-core": "browser automation types",
  typescript: "source analysis in scripts",
  tailwindcss: "styling",
  eslint: "lint configuration",
  "eslint-config-next": "lint configuration",
  "eslint-config-prettier": "lint configuration",
  "node:": "Node built-in modules",
};

const SOURCE = /\.(?:ts|tsx|mjs|css)$/;
const SCRIPT = /\.(?:js|jsx|cjs|mjs)$/;
/** Generated or vendored trees that the module rules do not cover. */
const OUTSIDE = /^(?:lib\/generated|proto|public|node_modules|\.next)\//;
const CONFIG_FILE = /^[^/]+\.config\.(?:ts|mjs)$/;

/** Files git tracks or would track, relative to web/studio. */
export function listedFiles() {
  const output = execFileSync("git", ["ls-files", "--cached", "--others", "--exclude-standard"], {
    cwd: studio,
    encoding: "utf8",
  });
  return [...new Set(output.split("\n"))].filter((path) => path && existsSync(join(studio, path))).sort();
}

/** Whether a file is JavaScript (.js, .jsx, .cjs or .mjs). */
export function isScript(path) {
  return SCRIPT.test(path);
}

/**
 * The module a Studio path belongs to: all of app/ is one module, documented by app/README.md; each
 * lib/generated/<tree> is one module, documented by its generator; any other file, JSON included, belongs
 * to its directory.
 */
function moduleOf(path) {
  if (path.startsWith("app/")) return "app";
  if (path.startsWith("lib/generated/")) return path.split("/").slice(0, 3).join("/");
  const directory = dirname(path);
  return directory === "." ? "" : directory;
}

/** Root configuration files (eslint.config.mjs, next.config.ts, ...) are tool inputs, not modules. */
export function isConfigFile(path) {
  return CONFIG_FILE.test(path);
}

function compilerOptions() {
  const configFile = join(studio, "tsconfig.json");
  const { config } = ts.readConfigFile(configFile, ts.sys.readFile);
  return ts.parseJsonConfigFileContent(config, ts.sys, studio, undefined, configFile).options;
}

function packageName(specifier) {
  if (specifier.startsWith("node:")) return "node:";
  const parts = specifier.split("/");
  return specifier.startsWith("@") ? parts.slice(0, 2).join("/") : parts[0];
}

/** Whether an import declaration or export carries only types, so nothing of it reaches the bundle. */
function typeOnly(node) {
  if (ts.isExportDeclaration(node)) return node.isTypeOnly;
  const clause = node.importClause;
  if (!clause) return false;
  if (clause.isTypeOnly) return true;
  const bindings = clause.namedBindings;
  return (
    !clause.name &&
    bindings !== undefined &&
    ts.isNamedImports(bindings) &&
    bindings.elements.length > 0 &&
    bindings.elements.every((element) => element.isTypeOnly)
  );
}

/** The leading string directives of a file ('use client', 'use server'). */
function directives(source) {
  const found = [];
  for (const statement of source.statements) {
    if (!ts.isExpressionStatement(statement) || !ts.isStringLiteral(statement.expression)) break;
    found.push(statement.expression.text);
  }
  return found;
}

const RPC_FUNCTIONS = new Set(["unary", "readUnary", "watch", "callRpc", "watchRpc"]);
const RPC_METHODS = new Set(["unary", "watch"]);

/** A business RPC call: unary/readUnary/watch, callRpc/watchRpc, or a backend's .unary/.watch. */
function rpcCall(node) {
  if (!ts.isCallExpression(node)) return false;
  const callee = node.expression;
  if (ts.isIdentifier(callee)) return RPC_FUNCTIONS.has(callee.text);
  return ts.isPropertyAccessExpression(callee) && RPC_METHODS.has(callee.name.text);
}

const exported = (node) => ts.getCombinedModifierFlags(node) & ts.ModifierFlags.Export;

/** The names of a file's value exports (functions, classes, variables, enums and value re-exports). */
function valueExports(source) {
  return source.statements.flatMap((statement) => {
    if (ts.isExportDeclaration(statement))
      return statement.isTypeOnly || !statement.exportClause || !ts.isNamedExports(statement.exportClause)
        ? []
        : statement.exportClause.elements.filter((element) => !element.isTypeOnly).map((element) => element.name.text);
    if (ts.isVariableStatement(statement) && exported(statement))
      return statement.declarationList.declarations.map((declaration) => declaration.name.getText(source));
    const value =
      ts.isFunctionDeclaration(statement) || ts.isClassDeclaration(statement) || ts.isEnumDeclaration(statement);
    return value && exported(statement) ? [statement.name?.text ?? "default"] : [];
  });
}

/** Specifiers, RPC calls, directives and value exports of one TypeScript or MJS file. */
function scanScript(path, text) {
  const kind = path.endsWith(".tsx") ? ts.ScriptKind.TSX : path.endsWith(".ts") ? ts.ScriptKind.TS : ts.ScriptKind.JS;
  const source = ts.createSourceFile(path, text, ts.ScriptTarget.Latest, true, kind);
  const specifiers = [];
  const rpcCalls = [];
  const visit = (node) => {
    if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier) {
      if (ts.isStringLiteral(node.moduleSpecifier))
        specifiers.push({ specifier: node.moduleSpecifier.text, typeOnly: typeOnly(node), dynamic: false });
    } else if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
      const [argument] = node.arguments;
      if (argument && ts.isStringLiteralLike(argument))
        specifiers.push({ specifier: argument.text, typeOnly: false, dynamic: true });
      else specifiers.push({ specifier: null, typeOnly: false, dynamic: true });
    } else if (ts.isImportTypeNode(node) && ts.isLiteralTypeNode(node.argument)) {
      const literal = node.argument.literal;
      if (ts.isStringLiteral(literal)) specifiers.push({ specifier: literal.text, typeOnly: true, dynamic: false });
    } else if (rpcCall(node)) {
      const [argument] = node.arguments;
      const { line } = source.getLineAndCharacterOfPosition(node.getStart(source));
      rpcCalls.push({ method: argument && ts.isStringLiteralLike(argument) ? argument.text : null, line: line + 1 });
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return { specifiers, rpcCalls, directives: directives(source), valueExports: valueExports(source) };
}

/** `@import` specifiers of one CSS file. */
function scanStylesheet(text) {
  const specifiers = [...text.matchAll(/@import\s+(?:url\()?["']([^"']+)["']/g)].map(([, specifier]) => ({
    specifier,
    typeOnly: false,
    dynamic: false,
  }));
  return { specifiers, rpcCalls: [], directives: [], valueExports: [] };
}

/** Returns a function that resolves one specifier to a Studio file or an npm package, or returns a problem. */
function importResolver(options, sourcePaths) {
  return (from, specifier) => {
    if (specifier === null) return { problem: "import() of a computed specifier" };
    const local = specifier.startsWith(".") || specifier.startsWith("@/");
    if (local && /\.(?:css|json)$/.test(specifier)) {
      const absolute = specifier.startsWith("@/")
        ? join(studio, specifier.slice(2))
        : resolve(studio, dirname(from), specifier);
      const path = relative(studio, absolute);
      return existsSync(absolute) ? { path } : { problem: `unresolved import ${specifier}` };
    }
    if (from.endsWith(".css") && !local) {
      const name = packageName(specifier);
      return existsSync(join(studio, "node_modules", name, "package.json"))
        ? { package: name }
        : { problem: `unresolved import ${specifier}` };
    }
    if (specifier.startsWith("node:")) return { package: "node:" };
    const { resolvedModule } = ts.resolveModuleName(specifier, join(studio, from), options, ts.sys);
    if (!resolvedModule) return { problem: `unresolved import ${specifier}` };
    const path = relative(studio, resolvedModule.resolvedFileName);
    if (resolvedModule.isExternalLibraryImport || path.startsWith("node_modules/"))
      return { package: packageName(specifier) };
    if (sourcePaths.has(path) || path.startsWith("lib/generated/")) return { path };
    return { problem: `import ${specifier} resolves outside Studio source (${path})` };
  };
}

/**
 * Returns {files, problems}. Each file records its text, module, line count, directives, RPC calls, value
 * exports and imports. An import names a Studio `path` with its `module` or an npm `package`, whether it
 * is type-only, and whether it is dynamic.
 */
export function loadModuleGraph() {
  const listed = listedFiles().filter((path) => !OUTSIDE.test(path) && (SOURCE.test(path) || isScript(path)));
  const resolveImport = importResolver(compilerOptions(), new Set(listed));
  const files = new Map();
  const problems = [];
  const usedPackages = new Set();
  for (const path of listed) {
    const text = readFileSync(join(studio, path), "utf8");
    const scanned = path.endsWith(".css") ? scanStylesheet(text) : scanScript(path, text);
    const imports = [];
    for (const { specifier, typeOnly, dynamic } of scanned.specifiers) {
      const target = resolveImport(path, specifier);
      if (target.problem) {
        problems.push(`${path}: ${target.problem}`);
        continue;
      }
      if (target.package) {
        usedPackages.add(target.package);
        if (!Object.hasOwn(EXTERNAL, target.package))
          problems.push(`${path}: classify package ${target.package} in EXTERNAL of scripts/module-graph.mjs`);
      }
      imports.push({ ...target, module: target.path && moduleOf(target.path), specifier, typeOnly, dynamic });
    }
    files.set(path, {
      path,
      text,
      module: moduleOf(path),
      lines: text.split("\n").length - (text.endsWith("\n") ? 1 : 0),
      directives: scanned.directives,
      rpcCalls: scanned.rpcCalls,
      valueExports: scanned.valueExports,
      imports,
    });
  }
  for (const name of Object.keys(EXTERNAL))
    if (!usedPackages.has(name))
      problems.push(`scripts/module-graph.mjs: EXTERNAL lists ${name}, which nothing imports`);
  return { files, problems };
}
