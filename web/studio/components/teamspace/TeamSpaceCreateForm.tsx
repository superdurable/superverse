// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useCallback, useRef, useState, type FormEvent } from "react";
import { createTeamSpace } from "@/lib/api/tenancy/team-space";
import { newRequestId } from "@/lib/api/transport/api-error";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";

/** Mail providers whose domain names no company: an address there names its Teamspace after the person. */
const PERSONAL_DOMAINS = new Set([
  "gmail.com",
  "googlemail.com",
  "outlook.com",
  "hotmail.com",
  "live.com",
  "icloud.com",
  "me.com",
  "yahoo.com",
  "proton.me",
  "protonmail.com",
  "aol.com",
  "qq.com",
  "163.com",
  "126.com",
]);

/** The second levels a country's domain registers companies under, as co in acme.co.uk. */
const SECOND_LEVELS = new Set(["co", "com", "org", "net", "ac", "gov", "edu", "ltd", "plc"]);

function capitalized(word: string): string {
  return word.charAt(0).toUpperCase() + word.slice(1);
}

/**
 * The name of an account's first Teamspace, from its email: a company's domain gives its name
 * (acme.com gives "Acme"); a personal mail provider gives the person's (gmail.com gives "Maya's
 * team" for Maya). Empty when the email names neither.
 */
export function teamSpaceNameFromEmail(email: string): string {
  const at = email.lastIndexOf("@");
  const local = email.slice(0, at).trim();
  const domain = email
    .slice(at + 1)
    .trim()
    .toLowerCase();
  if (at <= 0 || !domain) return "";
  if (PERSONAL_DOMAINS.has(domain)) {
    const first = local.split(/[.+]/)[0] ?? "";
    return first ? `${capitalized(first)}'s team` : "";
  }
  const labels = domain.split(".").filter(Boolean);
  // The second-level label: acme in acme.com and in eu.acme.com, or in acme.co.uk under a country's own.
  const countrySecondLevel = labels.length >= 3 && labels.at(-1)!.length === 2 && SECOND_LEVELS.has(labels.at(-2)!);
  return capitalized(labels.at(countrySecondLevel ? -3 : -2) ?? labels[0] ?? "");
}

/** One create attempt: its name and the request ID every retry of that name repeats. */
export interface TeamSpaceAttempt {
  name: string;
  requestId: string;
}

/**
 * Creates a Teamspace and opens it; its creator becomes its admin. One request ID serves every attempt
 * with the same name, so a retry after a lost answer opens the Teamspace it created. `first` seeds the
 * attempt, such as the account's first Teamspace with its stable request ID.
 */
export function useTeamSpaceCreation(first?: TeamSpaceAttempt, onCreated?: () => void) {
  const router = useRouter();
  const attempt = useRef<TeamSpaceAttempt | null>(first ?? null);
  const action = useSingleFlight<"create">();
  const { run } = action;
  const create = useCallback(
    (name: string) => {
      if (attempt.current?.name !== name) attempt.current = { name, requestId: newRequestId() };
      const { requestId } = attempt.current;
      void run("create", async (signal) => {
        const teamSpaceId = await createTeamSpace({ requestId, name }, signal);
        router.push(studioRoutes.teamSpace(teamSpaceId));
        onCreated?.();
      });
    },
    [onCreated, router, run],
  );
  return { create, busy: action.busy !== null, error: action.error, setError: action.setError };
}

/**
 * A new Teamspace's name, then the page opens it. `first` fills in the name an earlier attempt tried,
 * with its request ID and why it failed.
 */
export function TeamSpaceCreateForm({
  first,
  onCreated,
  onCancel,
}: {
  first?: TeamSpaceAttempt & { error: string };
  onCreated?: () => void;
  onCancel?: () => void;
}) {
  const [name, setName] = useState(first?.name ?? "");
  const creation = useTeamSpaceCreation(first, onCreated);
  const error = creation.error || (creation.busy ? "" : (first?.error ?? ""));
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return creation.setError("Give the Teamspace a name");
    creation.create(trimmed);
  };
  return (
    <form className="grid gap-3" onSubmit={submit}>
      <label className="grid gap-1.5 text-sm font-medium text-ink-dim">
        Teamspace name
        <input
          value={name}
          maxLength={80}
          autoFocus
          disabled={creation.busy}
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
      {error && (
        <Banner role="alert" className="text-left">
          {error}
        </Banner>
      )}
      <div className="flex gap-2">
        <Button type="submit" variant="strong" disabled={creation.busy}>
          {creation.busy ? "Creating…" : "Create Teamspace"}
        </Button>
        {onCancel && (
          <Button type="button" onClick={onCancel} disabled={creation.busy}>
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}
