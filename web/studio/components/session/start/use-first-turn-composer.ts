// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useState } from "react";

/** The composer's text: what the person typed, or took back into it from the queue, kept per Project. */
export function useComposerText(projectId: string) {
  const [typed, setTyped] = useState<{ projectId: string; text: string } | null>(null);
  const text = typed?.projectId === projectId ? typed.text : "";
  const setText = useCallback((next: string) => setTyped({ projectId, text: next }), [projectId]);
  return [text, setText] as const;
}
