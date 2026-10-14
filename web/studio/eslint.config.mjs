// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";
import prettier from "eslint-config-prettier/flat";

// The architectural boundaries (decision 0012). Zones resolve against basePath; a non-glob except is relative to
// its from, and a glob from needs glob except entries.
const FEATURES = ["./lib/account", "./lib/project", "./lib/session", "./lib/flow", "./lib/teamspace"];
const zones = [
  { target: "./lib", from: ["./components", "./app", "./mock"] },
  // Only app/ selects mock.
  { target: ["./components", "./lib", "./tests", "./scripts"], from: "./mock" },
  { target: ["./components", "./lib", "./mock", "./tests"], from: "./app" },
  { target: ["./components", "./lib/shared", "./lib/api", "./lib/browser", ...FEATURES], from: "./lib/server" },
  { target: "./lib/server", from: ["./lib/api", "./lib/browser", ...FEATURES] },
  { target: "./lib/shared", from: ["./lib/api", "./lib/browser", ...FEATURES] },
  { target: "./lib/api", from: ["./lib/browser", ...FEATURES] },
  // Features are independent.
  ...FEATURES.map((feature) => ({ target: feature, from: FEATURES.filter((other) => other !== feature) })),
  // The shared Agent UI is transport-free.
  { target: "./components/agent", from: "./lib/session", except: ["./agent-types.ts"] },
  {
    target: "./components/agent",
    from: ["./lib/api", "./lib/browser", "./lib/generated", "./lib/account", "./lib/project", "./lib/flow"],
  },
  { target: "./components/agent", from: "./components", except: ["./agent", "./ui"] },
  { target: ["./app", "./mock", "./components/!(agent)/**"], from: "./components/agent", except: ["./index.ts"] },
  // components/ui knows no product.
  { target: "./components/ui", from: "./components", except: ["./ui"] },
  { target: "./components/ui", from: ["./lib/api", ...FEATURES] },
  // Flow internals are reached only through components/flow/scope.
  { target: ["./app", "./mock", "./components/!(flow)/**"], from: "./components/flow", except: ["./scope"] },
  {
    target: "./components/flow/!(scope)/**",
    from: ["./components/flow/scope", "./lib/account", "./lib/project", "./lib/session"],
  },
  { target: "./components/flow/!(scope)/**", from: "./lib/api", except: ["./flow", "./transport"] },
  { target: "./components/flow/!(scope)/**", from: "./components", except: ["./flow", "./ui"] },
  // Tests may import lib/server.
  { target: "./tests", from: ["./components", "./lib/browser", ...FEATURES] },
];

const PARENT_IMPORT = { group: ["../*", ".."], message: "Import from outside the directory with @/." };
const BROWSER_GLOBAL = "is called only through lib/api/transport; components and providers use its clients.";

// Class names are styling; tests find Studio elements by role, label or test ID. Pages held in `app` are the
// applications under test, whose markup Studio does not own.
const LOCATOR = "CallExpression[callee.property.name='locator'][callee.object.name!='app']";
const CLASS_SELECTOR = "/(^|[\\s>+~,(])\\.[A-Za-z_]/";
const CLASS_SELECTOR_MESSAGE = "Find Studio elements by role, label or test ID, not by class.";

// A commit is shown as its version, "V12 · subject", through lib/shared/version; never as a cut hash.
const HASH_SLICE =
  "CallExpression[callee.property.name='slice'][arguments.0.value=0]:matches([arguments.1.value=7], [arguments.1.value=12])";
const COMMIT_NAME = "/[cC]ommit/";
const HASH_SLICE_MESSAGE = "Show a commit with versionLabel from lib/shared/version, not a cut hash.";

export default defineConfig([
  ...nextVitals,
  ...nextTs,
  {
    files: ["**/*.{ts,tsx,mjs}"],
    settings: { "import/resolver": { typescript: { project: `${import.meta.dirname}/tsconfig.json` } } },
    rules: {
      "import/no-restricted-paths": ["error", { basePath: import.meta.dirname, zones }],
      "import/no-unresolved": "error",
      "import/no-default-export": "error",
      "no-restricted-imports": ["error", { patterns: [PARENT_IMPORT] }],
      "no-restricted-globals": [
        "error",
        { name: "fetch", message: `fetch ${BROWSER_GLOBAL}` },
        { name: "EventSource", message: `EventSource ${BROWSER_GLOBAL}` },
      ],
    },
  },
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: { parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname } },
    rules: {
      "@typescript-eslint/consistent-type-imports": "error",
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-misused-promises": "error",
      "@typescript-eslint/switch-exhaustiveness-check": "error",
    },
  },
  // lib/shared and lib/api are framework-free.
  {
    files: ["lib/shared/**/*.ts", "lib/api/**/*.ts"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            PARENT_IMPORT,
            {
              group: ["react", "react/*", "react-dom", "react-dom/*", "next", "next/*", "node:*"],
              message: "lib/shared and lib/api use neither React, Next nor Node.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["lib/api/transport/**", "lib/server/**", "mock/**", "tests/**", "scripts/**"],
    rules: { "no-restricted-globals": "off" },
  },
  {
    files: ["app/**/{page,layout,template,loading,error,global-error,not-found,default}.tsx", "*.config.{ts,mjs}"],
    rules: { "import/no-default-export": "off" },
  },
  {
    files: ["app/**/*.{ts,tsx}", "components/**/*.{ts,tsx}", "lib/**/*.{ts,tsx}"],
    ignores: ["lib/shared/version/**"],
    rules: {
      "no-restricted-syntax": [
        "error",
        { selector: `${HASH_SLICE}[callee.object.name=${COMMIT_NAME}]`, message: HASH_SLICE_MESSAGE },
        { selector: `${HASH_SLICE}[callee.object.property.name=${COMMIT_NAME}]`, message: HASH_SLICE_MESSAGE },
      ],
    },
  },
  {
    files: ["tests/**/*.ts"],
    rules: {
      "no-restricted-syntax": [
        "error",
        { selector: `${LOCATOR} > Literal[value=${CLASS_SELECTOR}]`, message: CLASS_SELECTOR_MESSAGE },
        {
          selector: `${LOCATOR} > TemplateLiteral > TemplateElement[value.raw=${CLASS_SELECTOR}]`,
          message: CLASS_SELECTOR_MESSAGE,
        },
      ],
    },
  },
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    // Generated from api/superverse/v2/superverse.proto; the generation check enforces exact output.
    "lib/generated/protobuf/**",
  ]),
  prettier,
]);
