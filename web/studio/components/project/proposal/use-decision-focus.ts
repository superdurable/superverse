// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import type { EvolveProposalStatus__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveProposalStatus";

/**
 * Where keyboard focus goes after a person's decision on a card, whose Approve and Decline leave the page with it:
 * once the proposal is read again out of offered, to the card's state line (`stateRef`), which says where it went;
 * when the card leaves the page instead, to `onLeave`, such as the chat's composer, which also takes focus that was
 * in the card (`cardRef`) when it left. `decided` marks the decision the server answered.
 */
export function useDecisionFocus(status: EvolveProposalStatus__Output, onLeave?: () => void) {
  const stateRef = useRef<HTMLParagraphElement>(null);
  const cardRef = useRef<HTMLElement>(null);
  const awaiting = useRef(false);
  const leave = useRef(onLeave);
  useEffect(() => {
    leave.current = onLeave;
  });
  useEffect(() => {
    if (!awaiting.current || status === "EVOLVE_PROPOSAL_STATUS_OFFERED") return;
    awaiting.current = false;
    stateRef.current?.focus();
  }, [status]);
  // A layout cleanup runs before React removes the card, while the focus can still be seen inside it.
  useLayoutEffect(() => {
    const card = cardRef.current;
    return () => {
      if (awaiting.current || card?.contains(document.activeElement)) leave.current?.();
    };
  }, []);
  const decided = useCallback(() => {
    awaiting.current = true;
  }, []);
  return { stateRef, cardRef, decided };
}
