// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test as base, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";
import { decodedMappings, TraceMap } from "@jridgewell/trace-mapping";

// With SUPERVERSE_E2E_COVERAGE_DIR set, every passing spec records the Studio source files whose code ran in its
// pages, from the browser's block coverage and the production build's source maps (Native `--studio build` emits
// them). make studio-coverage-map merges the records into tests/e2e/coverage-map.json. Without the variable it
// records nothing, and it never changes an assertion.
const coverageDirectory = process.env.SUPERVERSE_E2E_COVERAGE_DIR;
// The webpack production build names a source webpack://_N_E/<path>, turbopack turbopack:///[project]/web/studio/<path>;
// only paths of files in the Studio tree are Studio sources, not the framework's own.
const studioSource = /^(?:webpack:\/\/_N_E\/(?:\.\/)?|turbopack:\/\/\/\[project\]\/web\/studio\/)([^?]+)(?:\?.*)?$/;

type CoverageEntry = Awaited<ReturnType<Page["coverage"]["stopJSCoverage"]>>[number];

// PageCoverage records the coverage of every page of the contexts it observes until the page closes.
class PageCoverage {
  private readonly running = new Map<Page, Promise<void>>();
  readonly entries: CoverageEntry[] = [];

  observe(context: BrowserContext) {
    for (const page of context.pages()) this.start(page);
    context.on("page", (page) => this.start(page));
    const close = context.close.bind(context);
    context.close = async (options) => {
      for (const page of context.pages()) await this.take(page);
      return close(options);
    };
  }

  private start(page: Page) {
    if (this.running.has(page)) return;
    this.begin(page);
    const close = page.close.bind(page);
    page.close = async (options) => {
      await this.take(page);
      return close(options);
    };
    // A document load discards the previous document's scripts with their coverage, so take it first. A load the
    // page starts itself, such as a redirect, loses the coverage of the document it leaves.
    const goto = page.goto.bind(page);
    page.goto = async (url, options) => {
      await this.restart(page);
      return goto(url, options);
    };
    const reload = page.reload.bind(page);
    page.reload = async (options) => {
      await this.restart(page);
      return reload(options);
    };
  }

  private begin(page: Page) {
    const started = page.coverage.startJSCoverage({ resetOnNavigation: false });
    // take awaits it; until then a page closed meanwhile must not surface as an unhandled rejection.
    started.catch(() => {});
    this.running.set(page, started);
  }

  private async restart(page: Page) {
    await this.take(page);
    if (!page.isClosed()) this.begin(page);
  }

  // take ends the page's coverage and keeps its entries.
  async take(page: Page) {
    const started = this.running.get(page);
    this.running.delete(page);
    if (!started || page.isClosed()) return;
    await started;
    this.entries.push(...(await page.coverage.stopJSCoverage()));
  }

  async takeAll(browser: Browser) {
    for (const context of browser.contexts()) for (const page of context.pages()) await this.take(page);
  }
}

// executedSources names the source of every source map segment whose generated code ran. V8 ranges nest, so with
// them sorted by start, then by end descending, a stack holds the ranges around an offset and its top one counts.
function executedSources(source: string, functions: CoverageEntry["functions"], map: TraceMap) {
  const lineStarts = [0];
  for (let index = 0; index < source.length; index++) if (source[index] === "\n") lineStarts.push(index + 1);
  const ranges = functions
    .flatMap((fn) => fn.ranges)
    .sort((a, b) => a.startOffset - b.startOffset || b.endOffset - a.endOffset);
  const open: typeof ranges = [];
  const sources = new Set<string>();
  let next = 0;
  decodedMappings(map).forEach((segments, line) => {
    for (const segment of segments) {
      if (segment.length === 1) continue;
      const offset = lineStarts[line] + segment[0];
      for (; next < ranges.length && ranges[next].startOffset <= offset; next++) {
        while (open.length && open[open.length - 1].endOffset <= ranges[next].startOffset) open.pop();
        open.push(ranges[next]);
      }
      while (open.length && open[open.length - 1].endOffset <= offset) open.pop();
      if (open.length && open[open.length - 1].count > 0) sources.add(map.resolvedSources[segment[1]]);
    }
  });
  return sources;
}

// sourceFiles names the Studio sources whose code ran, through each chunk's source map.
async function sourceFiles(entries: CoverageEntry[], studioRoot: string) {
  const files = new Set<string>();
  const maps = new Map<string, TraceMap | null>();
  for (const { url, source, functions } of entries) {
    if (!url.includes("/_next/static/") || !source) continue;
    if (!maps.has(url)) {
      const response = await fetch(`${url}.map`).catch(() => null);
      maps.set(url, response?.ok ? new TraceMap(await response.text()) : null);
    }
    const map = maps.get(url);
    if (!map) continue;
    for (const name of executedSources(source, functions, map)) {
      const match = studioSource.exec(name);
      if (match && !match[1].startsWith("node_modules/") && existsSync(path.join(studioRoot, match[1])))
        files.add(match[1]);
    }
  }
  return [...files].sort();
}

export const test = base.extend<{ specCoverage: void }>({
  specCoverage: [
    async ({ browser, context }, runTest, testInfo) => {
      if (!coverageDirectory) {
        await runTest();
        return;
      }
      const coverage = new PageCoverage();
      coverage.observe(context);
      const newContext = browser.newContext.bind(browser);
      browser.newContext = async (options) => {
        const created = await newContext(options);
        coverage.observe(created);
        return created;
      };
      try {
        await runTest();
      } finally {
        browser.newContext = newContext;
        await coverage.takeAll(browser);
      }
      // Only a passing spec vouches for the files it ran.
      if (testInfo.status !== "passed") return;
      // Paths come from the configured test directory, web/studio/tests/e2e, whatever the runner's working directory.
      const testDirectory = testInfo.project.testDir;
      const spec = path.relative(testDirectory, testInfo.file);
      const files = await sourceFiles(coverage.entries, path.resolve(testDirectory, "../.."));
      mkdirSync(coverageDirectory, { recursive: true });
      writeFileSync(
        path.join(coverageDirectory, `${spec.replaceAll("/", "__")}.json`),
        `${JSON.stringify({ spec, scripts: coverage.entries.length, files }, null, 2)}\n`,
      );
    },
    { auto: true },
  ],
});

export { expect } from "@playwright/test";
