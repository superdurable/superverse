// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useState } from "react";
import { listExamples } from "@/lib/api/example";
import { builtExample, type BuiltExample } from "./template-catalog";

/**
 * The built examples the server offers, read once per start page; none while they load or when the
 * read fails, so the placeholders and the composer always work.
 */
export function useExamples(): readonly BuiltExample[] {
  const [examples, setExamples] = useState<readonly BuiltExample[]>([]);
  useEffect(() => {
    const controller = new AbortController();
    listExamples(controller.signal)
      .then((listed) => {
        if (!controller.signal.aborted) setExamples(listed.map(builtExample));
      })
      .catch(() => {
        // The start page works without examples; the next visit reads them again.
      });
    return () => controller.abort();
  }, []);
  return examples;
}
