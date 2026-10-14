// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { Eyebrow } from "@/components/ui/Eyebrow";

export function TimerCard({ durationSeconds, reason }: { durationSeconds: number; reason: string }) {
  return (
    <section className="rounded-lg border border-line-soft bg-panel p-3 text-xs text-ink-dim">
      <Eyebrow>Durable timer</Eyebrow>
      <h2 className="mt-1 text-[13px] font-semibold text-ink">{durationSeconds}s</h2>
      <p className="mt-1">{reason}</p>
      <small className="mt-1 block text-[11px] text-ink-faint">Steering interrupts this wait at a safe boundary.</small>
    </section>
  );
}
