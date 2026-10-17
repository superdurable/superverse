// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState } from "react";

/** How long the person pauses inside a word before the grid ranks by it. */
const SETTLE_MS = 250;

/** A text that ends a word: a space or punctuation after it, or nothing at all. */
const WORD_BOUNDARY = /(^|[\s.,;:!?])$/;

/**
 * The description the example grid ranks by: what the person typed, taken at once when a word ends and
 * otherwise after a short pause, so the grid moves at word boundaries and not on every character.
 */
export function useSettledText(text: string): string {
  const [settled, setSettled] = useState(text);
  useEffect(() => {
    if (text === settled) return;
    const timer = window.setTimeout(() => setSettled(text), WORD_BOUNDARY.test(text) ? 0 : SETTLE_MS);
    return () => window.clearTimeout(timer);
  }, [settled, text]);
  return settled;
}
