// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * ModelIDRule names how the Go SDK's llm package validates a provider model
 * ID, spelled as llm.ModelIDRule's String method spells it: "body" for a model
 * sent in the JSON request body, and "pathSegment" for a model sent in a URL
 * path segment.
 */
export type ModelIDRule = "body" | "pathSegment";

/**
 * ModelIDValidation is the result of validateModelIDForRule: the canonical
 * model ID the provider would receive, or a message that never repeats the
 * value.
 */
export type ModelIDValidation = { isValid: true; modelId: string } | { isValid: false; message: string };

const pathSegmentModelIDPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;
const bodyModelIDMessage = "The model ID must be 1 to 256 printable ASCII characters without spaces.";
const pathSegmentModelIDMessage =
  'The model ID must start with a letter or digit and contain at most 128 letters, digits, ".", "_", or "-".';

/**
 * validateModelIDForRule applies sdkgo's llm.ModelIDRule.ValidateModelID in
 * the browser, so a picker rejects exactly the model IDs the connector's
 * generateText would reject as defect without a request.
 *
 * Both rules first trim the whitespace Go's strings.TrimSpace trims. "body"
 * then requires 1 to 256 characters from "!" (0x21) through "~" (0x7E), and
 * the trimmed value is the model ID. "pathSegment" then removes one leading
 * "models/" and requires `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, and the
 * remainder is the model ID. Any other rule is invalid.
 *
 * sdkgo/llm/llmtest/testdata/model_id_cases.json pins both rules; this
 * package's tests run every case.
 */
export function validateModelIDForRule(rule: ModelIDRule, value: string): ModelIDValidation {
  const model = trimGoWhitespace(value);
  switch (rule) {
    case "body":
      return model.length >= 1 && model.length <= 256 && isPrintableASCIIWithoutSpace(model)
        ? { isValid: true, modelId: model }
        : { isValid: false, message: bodyModelIDMessage };
    case "pathSegment": {
      const modelId = model.startsWith("models/") ? model.slice("models/".length) : model;
      return pathSegmentModelIDPattern.test(modelId)
        ? { isValid: true, modelId }
        : { isValid: false, message: pathSegmentModelIDMessage };
    }
    default:
      return { isValid: false, message: "The model ID rule is invalid." };
  }
}

/** trimGoWhitespace trims what Go's strings.TrimSpace trims; String.prototype.trim also trims U+FEFF and keeps U+0085. */
function trimGoWhitespace(value: string): string {
  let start = 0;
  let end = value.length;
  while (start < end && isGoWhitespace(value.charCodeAt(start))) start++;
  while (end > start && isGoWhitespace(value.charCodeAt(end - 1))) end--;
  return value.slice(start, end);
}

/** isGoWhitespace reports Go's unicode.IsSpace, whose code points are all in the Basic Multilingual Plane. */
function isGoWhitespace(code: number): boolean {
  return (
    (code >= 0x09 && code <= 0x0d) ||
    code === 0x20 ||
    code === 0x85 ||
    code === 0xa0 ||
    code === 0x1680 ||
    (code >= 0x2000 && code <= 0x200a) ||
    code === 0x2028 ||
    code === 0x2029 ||
    code === 0x202f ||
    code === 0x205f ||
    code === 0x3000
  );
}

function isPrintableASCIIWithoutSpace(model: string): boolean {
  for (let index = 0; index < model.length; index++) {
    const code = model.charCodeAt(index);
    if (code < 0x21 || code > 0x7e) return false;
  }
  return true;
}
