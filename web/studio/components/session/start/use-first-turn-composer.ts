// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useRef, useState } from "react";

/**
 * The composer's text: what the person typed, or took back into it from the queue, kept per
 * Project. Without it, the composer shows `draftText`, the first message that could not be delivered.
 */
export function useComposerText(projectId: string, draftText: string) {
  const [typed, setTyped] = useState<{ projectId: string; text: string } | null>(null);
  const text = typed?.projectId === projectId ? typed.text : draftText;
  const setText = useCallback((next: string) => setTyped({ projectId, text: next }), [projectId]);
  return [text, setText] as const;
}

/**
 * The composer's text box. The draft of a first message that could not be delivered (`draftKey`)
 * takes the focus once, when the composer can send it; the focus stays wherever the person moves it
 * afterwards.
 */
export function useDraftFocus(draftKey: string | null, disabledReason: string | null) {
  const input = useRef<HTMLTextAreaElement | null>(null);
  const focused = useRef<string | null>(null);
  useEffect(() => {
    if (draftKey === null || disabledReason !== null || focused.current === draftKey) return;
    focused.current = draftKey;
    input.current?.focus();
  }, [disabledReason, draftKey]);
  return input;
}
