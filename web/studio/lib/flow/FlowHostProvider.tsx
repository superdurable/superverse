// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, type ReactNode } from "react";

/** What the Studio page around the Flow views offers them, beyond the Flow API. */
interface FlowHost {
  /** The version the views show; null outside the Builder. */
  environment: "preview" | "live" | null;
  /** Builders may stop a run and rerun it from a Step. */
  canIntervene: boolean;
  /** Hands the Project's chat something to ask about, with optional text; absent outside the Builder. */
  askAgent?: (about: string, text?: string) => void;
  /** The Project's Teamspace, whose connections its connection names select; absent outside the Builder. */
  teamSpaceId?: string | null;
}

const FlowHostContext = createContext<FlowHost>({ environment: null, canIntervene: true });

/** Supplies the Builder's version, the reader's role and the chat to the Flow views below. */
export function FlowHostProvider({ host, children }: { host: FlowHost; children: ReactNode }) {
  return <FlowHostContext.Provider value={host}>{children}</FlowHostContext.Provider>;
}

export function useFlowHost(): FlowHost {
  return useContext(FlowHostContext);
}
