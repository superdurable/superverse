// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Dropdown } from "@/components/ui/menu/Dropdown";

/** One member as a table shows them. */
export interface MemberRow<R extends string> {
  accountId: string;
  email: string;
  role: R;
  /** Why the row reads differently, such as the only admin, or a member who left the TeamSpace. */
  note?: string;
  /** Nothing about the row can change, such as the only admin or owner. */
  fixed?: boolean;
  muted?: boolean;
}

/**
 * People with one role each, one line per person. Those who may change them get a labelled role dropdown and
 * Remove on every row that can change; everyone else reads the roles. `describe` says what a role allows in each
 * choice of the dropdown and in the hint of a role that only reads, and `roleNote` heads the dropdown's menu.
 */
export function MemberTable<R extends string>({
  label,
  rows,
  roles,
  selfId,
  editable,
  busy,
  describe,
  roleNote,
  onRole,
  onRemove,
}: {
  label: string;
  rows: MemberRow<R>[];
  /** Every role in nesting order, with its word. */
  roles: readonly { value: R; word: string }[];
  selfId: string | null;
  editable: boolean;
  busy: boolean;
  describe?: (role: R) => string;
  roleNote?: string;
  onRole: (accountId: string, role: R) => void;
  onRemove: (accountId: string) => void;
}) {
  const word = (role: R) => roles.find((item) => item.value === role)?.word ?? role;
  return (
    <div className="overflow-x-auto rounded-[10px] border border-line-soft bg-panel">
      <table aria-label={label} className="w-full text-left text-[13px]">
        <thead className="text-[11px] tracking-[0.06em] text-ink-faint uppercase">
          <tr className="border-b border-line-soft">
            <th className="px-3.5 py-2 font-normal">Person</th>
            <th className="px-3.5 py-2 font-normal">Role</th>
            {editable && (
              <th className="px-3.5 py-2 font-normal">
                <span className="sr-only">Actions</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr
              key={row.accountId}
              className={cx("border-b border-line-faint last:border-b-0", row.muted && "text-ink-faint")}
            >
              <td className="px-3.5 py-2.5">
                <span className="wrap-anywhere">{row.email}</span>
                {row.accountId === selfId && <span className="ml-1.5 text-ink-faint">you</span>}
                {row.note && <small className="block text-[11.5px] text-ink-faint">{row.note}</small>}
              </td>
              <td className="px-3.5 py-2.5">
                {editable && !row.fixed ? (
                  <Dropdown
                    label={`Role of ${row.email}`}
                    inline
                    value={row.role}
                    disabled={busy}
                    onChange={(next) => onRole(row.accountId, next)}
                    note={roleNote}
                    title={describe?.(row.role)}
                    items={roles.map((item) => ({ value: item.value, label: item.word, sub: describe?.(item.value) }))}
                  />
                ) : (
                  <span title={describe?.(row.role)}>{word(row.role)}</span>
                )}
              </td>
              {editable && (
                <td className="px-3.5 py-2.5 text-right">
                  {!row.fixed && (
                    <Button
                      size="sm"
                      variant="quiet"
                      disabled={busy}
                      aria-label={`Remove ${row.email}`}
                      onClick={() => onRemove(row.accountId)}
                    >
                      Remove
                    </Button>
                  )}
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
