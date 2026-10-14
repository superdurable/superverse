// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/*
 * Studio's styling rules (`npm run check:styles`, decision 0012), read after `next build`. CSS lives only
 * in app/, each sheet within its selector allowlist and without @apply. Every static class token a
 * component writes exists in the built CSS, so a class that styles nothing fails instead of silently
 * rendering unstyled. Every custom property a source reads is defined, and class strings are never
 * glued together by concatenation.
 */
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import ts from "typescript";
import { listedFiles, studio } from "./module-graph.mjs";

/** The only stylesheets, each with the class selectors it may use. */
const SHEETS = {
  // Tokens, the cascade and element defaults: no class selectors at all.
  "app/globals.css": () => false,
  // React Flow internals: the library's own classes and its state classes; everything else is [data-*].
  "app/react-flow.css": (name) => /^react-flow(?:__[\w-]+)?$/.test(name) || REACT_FLOW_STATES.has(name),
};
const REACT_FLOW_STATES = new Set(["selected", "animated", "dragging", "selectable", "draggable"]);

/** Class tokens that style nothing on purpose: React Flow's interaction opt-outs and Tailwind's group and peer markers. */
const BEHAVIOR_CLASS = /^(?:nodrag|nopan|nowheel|(?:group|peer)(?:\/[\w-]+)?)$/;

/** Custom properties that components set at runtime with style.setProperty or an inline style. */
const RUNTIME_PROPERTIES = [
  "--zoom-comp",
  "--edge-boost",
  "--timeline-link-rail-width",
  "--sidebar-w",
  "--chat-w",
  "--tools-w",
  "--v2-list-w",
  "--v2-drawer-w",
  "--v2-panel-w",
  "--run-sidebar-width",
  "--popover-x",
  "--popover-y",
  "--popover-max-h",
  "--popover-anchor-w",
];

/** Directories whose TypeScript renders or styles UI. */
const STYLED_SOURCE = /^(?:app|components|lib|mock)\/.*\.tsx?$/;
const GENERATED = /^lib\/generated\//;

const problems = [];

function builtCss() {
  const directory = join(studio, ".next/static/css");
  let names;
  try {
    names = readdirSync(directory).filter((name) => name.endsWith(".css"));
  } catch {
    names = [];
  }
  if (names.length === 0) {
    console.error("check-styles reads the built CSS: run `next build` first");
    process.exit(1);
  }
  return names.map((name) => readFileSync(join(directory, name), "utf8")).join("\n");
}

const unescapeCss = (text) =>
  text.replace(/\\([0-9a-fA-F]{1,6})\s?/g, (_, hex) => String.fromCodePoint(parseInt(hex, 16))).replace(/\\(.)/g, "$1");

/** Every class name a selector of the CSS mentions. */
function classNames(css) {
  return new Set(
    [...css.matchAll(/\.((?:\\[0-9a-fA-F]{1,6}\s?|\\.|[\w-]|[^\x00-\x7f])+)/g)].map(([, name]) => unescapeCss(name)),
  );
}

/** Every custom property the CSS declares. */
function declaredProperties(css) {
  return new Set([...css.matchAll(/(--[\w-]+)\s*:/g)].map(([, name]) => name));
}

/** The selector lists of a stylesheet's style rules; at-rule preludes and declarations are skipped. */
function selectorsOf(css) {
  const text = css.replace(/\/\*[\s\S]*?\*\//g, "");
  const selectors = [];
  let start = 0;
  for (let index = 0; index < text.length; index++) {
    const char = text[index];
    if (char === "{") {
      const prelude = text.slice(start, index).trim();
      if (prelude && !prelude.startsWith("@")) selectors.push(prelude);
      start = index + 1;
    } else if (char === "}" || char === ";") start = index + 1;
  }
  return selectors;
}

function checkSheets(sheets) {
  for (const [path, text] of sheets) {
    const allowed = SHEETS[path];
    if (!allowed) {
      problems.push(`${path}: CSS lives only in ${Object.keys(SHEETS).join(" and ")}; style with utilities`);
      continue;
    }
    if (/@apply\b/.test(text)) problems.push(`${path}: no @apply; write the utilities in the component`);
    for (const selector of selectorsOf(text))
      for (const [, name] of selector.matchAll(/\.([A-Za-z_-][\w-]*)/g))
        if (!allowed(name)) problems.push(`${path}: selector ${selector} uses .${name}, outside the sheet's allowlist`);
  }
}

const isClassName = (name) => name === "className" || name.endsWith("ClassName");
const isCx = (node) => ts.isCallExpression(node) && ts.isIdentifier(node.expression) && node.expression.text === "cx";

/**
 * Checks one source file's class contexts: className attributes and properties and cx() arguments.
 * Returns their static tokens with line numbers; reports concatenation and templates that can glue.
 */
function classTokensOf(path, text) {
  const source = ts.createSourceFile(path, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const tokens = [];
  const lineOf = (node) => source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
  const add = (node, literal) => {
    for (const token of literal.split(/\s+/)) if (token) tokens.push({ token, line: lineOf(node) });
  };
  const collect = (node) => {
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) add(node, node.text);
    else if (ts.isTemplateExpression(node)) checkTemplate(node);
    else if (ts.isConditionalExpression(node)) [node.whenTrue, node.whenFalse].forEach(collect);
    else if (ts.isJsxExpression(node) && node.expression) collect(node.expression);
    else if (
      ts.isParenthesizedExpression(node) ||
      ts.isAsExpression(node) ||
      ts.isSatisfiesExpression(node) ||
      ts.isNonNullExpression(node)
    )
      collect(node.expression);
    else if (ts.isBinaryExpression(node)) {
      const operator = node.operatorToken.kind;
      if (operator === ts.SyntaxKind.PlusToken)
        problems.push(`${path}:${lineOf(node)}: class strings are joined with cx(), never with +`);
      else if (operator === ts.SyntaxKind.AmpersandAmpersandToken) collect(node.right);
      else if (operator === ts.SyntaxKind.BarBarToken || operator === ts.SyntaxKind.QuestionQuestionToken)
        [node.left, node.right].forEach(collect);
    }
  };
  // A literal part next to ${} must be separated from it by whitespace, or two classes can be glued.
  const checkTemplate = (node) => {
    const parts = [node.head, ...node.templateSpans.map((span) => span.literal)];
    parts.forEach((part, index) => {
      const glued =
        (index > 0 && !/^\s/.test(part.text) && !(index === parts.length - 1 && part.text === "")) ||
        (index < parts.length - 1 && !/\s$/.test(part.text) && !(index === 0 && part.text === ""));
      if (glued)
        problems.push(
          `${path}:${lineOf(node)}: a class template part next to \${} needs whitespace; pass separate cx() arguments`,
        );
      add(node, part.text);
    });
  };
  const visit = (node) => {
    if (ts.isJsxAttribute(node) && isClassName(node.name.getText(source)) && node.initializer)
      collect(node.initializer);
    else if (ts.isPropertyAssignment(node) && isClassName(node.name.getText(source))) collect(node.initializer);
    else if (isCx(node)) node.arguments.forEach(collect);
    ts.forEachChild(node, visit);
  };
  visit(source);
  return tokens;
}

/** Custom properties a source reads: var(--x) and Tailwind's shorthand utility-(--x). */
function readProperties(text) {
  return [
    ...[...text.matchAll(/var\(\s*(--[\w-]+)/g)].map(([, name]) => name),
    ...[...text.matchAll(/[\w\]]-\((?:[a-z-]+:)?(--[\w-]+)[,)]/g)].map(([, name]) => name),
  ];
}

const css = builtCss();
const built = classNames(css);
const defined = new Set([...declaredProperties(css), ...RUNTIME_PROPERTIES]);
const files = listedFiles().filter((path) => !GENERATED.test(path));
const sheets = files
  .filter((path) => path.endsWith(".css"))
  .map((path) => [path, readFileSync(join(studio, path), "utf8")]);
checkSheets(sheets);

const runtimeSet = new Set();
let checkedTokens = 0;
for (const path of files.filter((file) => STYLED_SOURCE.test(file))) {
  const text = readFileSync(join(studio, path), "utf8");
  for (const { token, line } of classTokensOf(path, text)) {
    checkedTokens++;
    if (!built.has(token) && !BEHAVIOR_CLASS.test(token))
      problems.push(`${path}:${line}: class ${token} is not in the built CSS; it styles nothing`);
  }
  for (const name of readProperties(text))
    if (!defined.has(name)) problems.push(`${path}: reads ${name}, which no stylesheet or runtime list defines`);
  for (const name of RUNTIME_PROPERTIES) if (text.includes(`"${name}"`)) runtimeSet.add(name);
}
for (const [path, text] of sheets)
  for (const name of readProperties(text))
    if (!defined.has(name)) problems.push(`${path}: reads ${name}, which is defined nowhere`);
for (const name of RUNTIME_PROPERTIES)
  if (!runtimeSet.has(name))
    problems.push(`scripts/check-styles.mjs: RUNTIME_PROPERTIES lists ${name}, which no component sets`);

if (problems.length) {
  console.error([...new Set(problems)].join("\n"));
  process.exit(1);
}
console.log(`Studio styles: ${sheets.length} stylesheets and ${checkedTokens} class tokens match the built CSS`);
