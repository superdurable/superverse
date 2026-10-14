// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import type { PreviewStartRecord__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewStartRecord";
import type { ReleaseCandidate__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseCandidate";
import { validationText, versionValidation } from "@/lib/project/setup/preview-validation";
import { commitDetail, versionNumber } from "@/lib/shared/version/version";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";

/**
 * The version to publish: the chosen one, by default the newest ahead of Live, with the others folded
 * behind "Another version (N more ahead of Live)". Each names its subject and its Preview validation:
 * "ran on Preview at 10:02", or "not in the Preview record" when no READY start of it is among the
 * Preview's bounded record, which never claims it never ran.
 */
export function VersionChoice({
  ahead,
  chosen,
  starts,
  onChoose,
}: {
  /** The versions ahead of Live, newest first. */
  ahead: readonly ReleaseCandidate__Output[];
  chosen: string;
  starts: readonly PreviewStartRecord__Output[];
  onChoose: (commit: string) => void;
}) {
  const [more, setMore] = useState(false);
  const shown = more ? ahead : ahead.filter((version) => version.sourceCommit === chosen);
  return (
    <section aria-label="Version">
      <h3 className="px-1 pb-0.5 text-[11px] tracking-[0.07em] text-ink-faint uppercase" id="publish-version-label">
        Version
      </h3>
      <div role="radiogroup" aria-labelledby="publish-version-label" className="grid gap-0.5">
        {shown.map((version) => {
          const on = version.sourceCommit === chosen;
          const ran = versionValidation(starts, version.sourceCommit);
          return (
            <button
              key={version.sourceCommit}
              type="button"
              role="radio"
              aria-checked={on}
              data-testid={`publish-version-${version.sourceCommit}`}
              title={commitDetail(version.sourceCommit)}
              onClick={() => onChoose(version.sourceCommit)}
              className={cx(
                "flex w-full items-start gap-2.5 rounded-lg px-2 py-1.75 text-left hover:bg-hover focus-visible:shadow-[inset_0_0_0_2px_var(--color-accent-rule)] focus-visible:outline-none",
                on && "bg-accent-wash",
              )}
            >
              <span
                aria-hidden="true"
                className={cx(
                  "mt-0.5 size-3.5 shrink-0 rounded-full",
                  on ? "border-4 border-accent" : "border-[1.5px] border-mark",
                )}
              />
              <span className="flex min-w-0 flex-1 flex-col gap-px">
                <span className="text-[13px] text-ink">
                  <b className="font-semibold">{versionNumber(version.version)}</b>
                  {version.sourceCommit === ahead[0]?.sourceCommit && " · latest"}
                </span>
                <small className="text-[11.5px] leading-snug text-ink-faint">
                  {version.subject || "Untitled change"} ·{" "}
                  <span className={ran.validated ? "" : "text-busy"}>{validationText(ran)}</span>
                </small>
              </span>
            </button>
          );
        })}
      </div>
      {ahead.length > 1 && (
        <button
          type="button"
          aria-expanded={more}
          onClick={() => setMore((value) => !value)}
          className="mt-0.5 ml-1 inline-flex items-center gap-1.5 text-[12.5px] text-ink-dim hover:text-ink"
        >
          <Glyph name="chevronDown" className={cx("size-3.5 transition-transform", more && "rotate-180")} />
          {more ? "Fewer versions" : `Another version (${ahead.length - 1} more ahead of Live)`}
        </button>
      )}
    </section>
  );
}
