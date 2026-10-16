// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState, type ReactNode } from "react";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { CHOICE, CHOICE_INPUT, STEP_MARK } from "@/components/flow/frame/workspace-styles";

/** A step's heading, numbered while a connection is being set up. */
export function StepLegend({ number, children }: { number: number | null; children: ReactNode }) {
  return (
    <legend className="mb-1 flex items-center gap-2 text-[13px] font-semibold text-ink">
      {number !== null && (
        <span aria-hidden="true" className={STEP_MARK}>
          {number}
        </span>
      )}
      {children}
    </legend>
  );
}

/** A part of the form folded behind its label until the reader opens it. */
export function Disclosure({ label, children }: { label: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="grid gap-2.5">
      <button
        aria-expanded={open}
        className="flex w-fit items-center gap-1.5 text-xs text-ink-dim hover:text-ink"
        onClick={() => setOpen((current) => !current)}
        type="button"
      >
        <Glyph name="chevronDown" className={cx("size-3.5 transition-transform", open && "rotate-180")} />
        {label}
      </button>
      {open && children}
    </div>
  );
}

/**
 * What a connector's platform installation shares beyond the Teamspace, by connector ID: every
 * Teamspace that connects one Slack workspace through Superverse's app shares its one installation.
 */
const SHARED_PLATFORM_INSTALLATION: Record<string, string> = {
  slack:
    "Teamspaces that connect the same Slack workspace with Superverse's app share its bot: its channels, its events and an uninstall. Use your own app to keep them apart.",
};

/** Whether an authorization uses the platform's own OAuth application or the person's. */
export function OAuthClientChoice({
  connectorId,
  platform,
  onChange,
}: {
  connectorId: string;
  platform: boolean;
  onChange: (platform: boolean) => void;
}) {
  const shared = SHARED_PLATFORM_INSTALLATION[connectorId];
  return (
    <div className="grid gap-1.5" role="radiogroup" aria-label="OAuth application">
      {[
        { value: true, label: "Superverse's app", note: "No provider app to create; Superverse keeps its client." },
        { value: false, label: "Your own app", note: "Enter the client of an app you registered with the provider." },
      ].map((choice) => (
        <label className={CHOICE} key={choice.label}>
          <input
            className={CHOICE_INPUT}
            checked={platform === choice.value}
            name="connector-oauth-client"
            onChange={() => onChange(choice.value)}
            type="radio"
          />
          <span className="grid min-w-0 gap-0.5">
            <b className="text-[13px] font-semibold text-ink">{choice.label}</b>
            <small className="text-[11px] text-ink-faint">{choice.note}</small>
          </span>
        </label>
      ))}
      {platform && shared && <p className="text-[11px] text-ink-faint">{shared}</p>}
    </div>
  );
}
