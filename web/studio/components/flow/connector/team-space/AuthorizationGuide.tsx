// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useState } from "react";
import type { ManifestGuide } from "@/lib/api/flow/connector";
import { linkifiedDescription } from "@/components/flow/connector/manifest/ManifestFormField";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { LABEL, LINK } from "@/components/flow/frame/workspace-styles";

/** Where to create the provider application an authorization method needs, and the redirect URI it must allow. */
export function AuthorizationGuide({ guide, redirectURI }: { guide: ManifestGuide; redirectURI?: string }) {
  return (
    <section
      aria-label="Authorization guide"
      className="grid gap-1 rounded-md border border-line-faint bg-bg px-2.5 py-2 text-xs text-ink-dim"
    >
      <h4 className={cx(LABEL, "font-semibold")}>Authorization guide</h4>
      <p>
        Start at{" "}
        <a className={LINK} href={guide.startURL} rel="noreferrer" target="_blank">
          {guide.startURL}
        </a>
      </p>
      <ol className="grid list-decimal gap-0.75 pl-4.5">
        {guide.steps.map((step, index) => (
          <li key={`${index}:${step}`}>{linkifiedDescription(step)}</li>
        ))}
      </ol>
      {redirectURI && (
        <div className="grid gap-1 pt-1">
          <span className={LABEL}>Redirect URI</span>
          <CopyValue label="Redirect URI" value={redirectURI} />
        </div>
      )}
    </section>
  );
}

function CopyValue({ label, value }: { label: string; value: string }) {
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <code className="text-[11px]/[1.4] wrap-anywhere text-ink-dim">{value}</code>
      <CopyButton label={label} value={value} />
    </span>
  );
}

function CopyButton({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      aria-label={`${copied ? "Copied" : "Copy"} ${label}`}
      className="flex-none"
      size="sm"
      onClick={() => {
        void navigator.clipboard.writeText(value).then(() => setCopied(true));
      }}
    >
      {copied ? "Copied" : "Copy"}
    </Button>
  );
}
