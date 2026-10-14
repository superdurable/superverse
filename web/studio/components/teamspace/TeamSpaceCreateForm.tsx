// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useRef, useState, type FormEvent } from "react";
import { createTeamSpace } from "@/lib/api/tenancy/team-space";
import { newRequestId } from "@/lib/api/transport/api-error";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

/**
 * A new TeamSpace's name; its creator becomes its admin, and the page then opens it. One request ID serves
 * every attempt with the same name, so a retry after a lost answer opens the TeamSpace it created.
 */
export function TeamSpaceCreateForm({ onCreated, onCancel }: { onCreated?: () => void; onCancel?: () => void }) {
  const router = useRouter();
  const [name, setName] = useState("");
  const attempt = useRef<{ name: string; requestId: string } | null>(null);
  const action = useSingleFlight<"create">();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return action.setError("Give the TeamSpace a name");
    if (attempt.current?.name !== trimmed) attempt.current = { name: trimmed, requestId: newRequestId() };
    const { requestId } = attempt.current;
    void action.run("create", async (signal) => {
      const teamSpaceId = await createTeamSpace({ requestId, name: trimmed }, signal);
      router.push(studioRoutes.teamSpace(teamSpaceId));
      onCreated?.();
    });
  };
  return (
    <form className="grid gap-3" onSubmit={submit}>
      <label className="grid gap-1.5 text-sm font-medium text-ink-dim">
        TeamSpace name
        <input
          value={name}
          maxLength={80}
          autoFocus
          disabled={action.busy !== null}
          onChange={(event) => setName(event.target.value)}
          placeholder="Customer Ops"
          className={cx(
            "w-full rounded-lg border border-line bg-panel px-3 py-2 text-base font-normal text-ink placeholder:text-ink-faint focus:border-accent focus:shadow-(--focus-ring) focus:outline-none disabled:opacity-50",
          )}
        />
      </label>
      <p className="text-[12.5px] text-ink-faint">
        You become its admin. Add people from People once it exists; each gets a role per project.
      </p>
      {action.error && (
        <Banner role="alert" className="text-left">
          {action.error}
        </Banner>
      )}
      <div className="flex gap-2">
        <Button type="submit" variant="strong" disabled={action.busy !== null}>
          {action.busy ? "Creating…" : "Create TeamSpace"}
        </Button>
        {onCancel && (
          <Button type="button" onClick={onCancel} disabled={action.busy !== null}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}
