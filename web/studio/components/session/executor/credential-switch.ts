// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { MouseEvent } from "react";

/** Keep a draft until the user explicitly discards it when leaving Platform keys. */
export function confirmCredentialSwitch(event: MouseEvent<HTMLElement>) {
  const target = event.target as HTMLElement;
  if (!target.closest('[data-discard-credential], input[value="local"]')) return;
  const panelId = event.currentTarget.getAttribute("aria-controls");
  const scope = (panelId && document.getElementById(panelId)) || event.currentTarget;
  if (scope.querySelector('[data-credential-busy="true"]')) {
    event.preventDefault();
    event.stopPropagation();
    return;
  }
  const draft = scope.querySelector<HTMLElement>('[data-credential-dirty="true"]');
  if (!draft) return;
  const discard = draft.querySelector<HTMLButtonElement>("[data-discard-key]");
  if (!discard || discard.disabled || !window.confirm("Discard your unsaved key changes?")) {
    event.preventDefault();
    event.stopPropagation();
    return;
  }
  discard.click();
}
