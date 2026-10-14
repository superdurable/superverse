// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Components, ExtraProps } from "react-markdown";
import type { Node, Parent } from "unist";
import { cx } from "@/components/ui/cx";

const BLOCK = cx("my-2");
const HEADING = cx("mt-3 mb-1.5 font-semibold text-ink");
const CELL = cx("min-w-30 border border-line-soft px-2.5 py-2 text-left align-top break-normal wrap-normal");

/**
 * An element's Markdown attributes with our look added to its classes. Every attribute passes
 * through, because GFM footnotes and task lists depend on them (the footnote label's `sr-only` and
 * id, the reference's id and aria-describedby, the back link's aria-label); only react-markdown's
 * `node` stays off the DOM.
 */
function withLook<Props extends ExtraProps & { className?: string }>(props: Props, look: string): Omit<Props, "node"> {
  const { node, ...attributes } = props;
  void node;
  return { ...attributes, className: cx(look, props.className) };
}

const components: Components = {
  p: (props) => <p {...withLook(props, BLOCK)} />,
  h1: (props) => <h1 {...withLook(props, HEADING)} />,
  h2: (props) => <h2 {...withLook(props, HEADING)} />,
  h3: (props) => <h3 {...withLook(props, HEADING)} />,
  h4: (props) => <h4 {...withLook(props, HEADING)} />,
  ul: (props) => <ul {...withLook(props, cx(BLOCK, "list-disc pl-5"))} />,
  ol: (props) => <ol {...withLook(props, cx(BLOCK, "list-decimal pl-5"))} />,
  blockquote: (props) => <blockquote {...withLook(props, cx(BLOCK, "border-l-3 border-line pl-3"))} />,
  // Accent means both link and running, so the underline carries "clickable".
  a: (props) => <a {...withLook(props, cx("text-accent underline"))} />,
  code: (props) => <code {...withLook(props, cx("rounded-sm bg-well px-[0.35em] py-[0.1em] text-[0.92em]"))} />,
  // react-markdown passes no `inline` prop, so the block resets its own code element.
  pre: (props) => (
    <pre
      {...withLook(
        props,
        cx(
          "mt-2 max-w-full overflow-auto rounded-lg bg-well p-2.5 whitespace-pre-wrap text-ink [&>code]:bg-transparent [&>code]:p-0",
        ),
      )}
    />
  ),
  table: (props) => (
    <div className="my-3 w-full max-w-full overflow-x-auto overscroll-x-contain">
      <table {...withLook(props, cx("w-max min-w-[min(560px,100%)] border-collapse wrap-normal"))} />
    </div>
  ),
  th: (props) => <th {...withLook(props, cx(CELL, "bg-well"))} />,
  td: (props) => <td {...withLook(props, CELL)} />,
};

const breakTagPattern = /^<br\s*\/?\s*>$/i;

export function MarkdownContent({ value }: { value: string }) {
  return (
    <div className="max-w-full min-w-0 leading-[1.55] wrap-anywhere text-ink-dim *:first:mt-0 *:last:mb-0">
      <ReactMarkdown components={components} remarkPlugins={[remarkGfm, replaceBreakTags]}>
        {value}
      </ReactMarkdown>
    </div>
  );
}

function replaceBreakTags() {
  return (tree: Node): void => {
    replaceBreakTagNodes(tree);
  };
}

function replaceBreakTagNodes(node: Node): void {
  if (!isParent(node)) return;
  for (let index = 0; index < node.children.length; index += 1) {
    const child = node.children[index];
    if (child === undefined) continue;
    if (isBreakTag(child)) {
      node.children[index] = { type: "break" };
      continue;
    }
    replaceBreakTagNodes(child);
  }
}

function isParent(node: Node): node is Parent {
  return "children" in node && Array.isArray(node.children);
}

function isBreakTag(node: Node): node is Node & { value: string } {
  return node.type === "html" && "value" in node && typeof node.value === "string" && breakTagPattern.test(node.value);
}
