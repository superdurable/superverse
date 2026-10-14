// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";

interface ConversationClock {
  nowMs: number;
  listeners: Set<() => void>;
}

interface ConversationClockValue {
  clock: ConversationClock;
  /** Whether the conversation is live, so the clock ticks. */
  active: boolean;
}

const ConversationClockContext = createContext<ConversationClockValue | null>(null);

// One disposable clock per conversation. Only live work logs subscribe; completed
// messages and their work presentation are not rendered again on each tick.
export function ConversationClockProvider({ active, children }: { active: boolean; children: ReactNode }) {
  const [clock] = useState<ConversationClock>(() => ({ nowMs: Date.now(), listeners: new Set() }));
  useEffect(() => {
    if (!active) return;
    const interval = window.setInterval(() => {
      clock.nowMs = Date.now();
      clock.listeners.forEach((notify) => notify());
    }, 1_000);
    return () => window.clearInterval(interval);
  }, [active, clock]);
  const value = useMemo(() => ({ clock, active }), [clock, active]);
  return <ConversationClockContext.Provider value={value}>{children}</ConversationClockContext.Provider>;
}

/**
 * The conversation clock's time, re-rendering the caller on each tick while the conversation is live and the caller
 * has live work; null otherwise. The server renders a ticking caller at 0.
 */
export function useConversationClock(hasLiveWork: boolean): number | null {
  const { clock, active } = useContext(ConversationClockContext) ?? missingProvider();
  const ticking = active && hasLiveWork;
  const subscribe = useCallback(
    (notify: () => void) => {
      if (!ticking) return () => {};
      clock.listeners.add(notify);
      return () => {
        clock.listeners.delete(notify);
      };
    },
    [ticking, clock],
  );
  const snapshot = useCallback(() => (ticking ? clock.nowMs : null), [ticking, clock]);
  const serverSnapshot = useCallback(() => (ticking ? 0 : null), [ticking]);
  return useSyncExternalStore(subscribe, snapshot, serverSnapshot);
}

function missingProvider(): never {
  throw new Error("useConversationClock is used outside a ConversationClockProvider");
}
