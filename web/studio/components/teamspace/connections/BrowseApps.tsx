// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import type { ConnectorCatalogEntry } from "@/lib/api/flow/team-space-connection";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { LazyTeamSpaceConnectionEditor } from "@/components/flow/scope/LazyFlowViews";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { AppLogo } from "./AppLogo";
import { connectionStatus, freeName } from "./connection-words";
import { useConnections } from "./use-connections";

const SEARCH =
  "w-full rounded-lg border border-line bg-panel px-3 py-2 text-[13px] text-ink placeholder:text-ink-faint focus:border-accent focus:shadow-(--focus-ring) focus:outline-none";

/**
 * Add an app: the catalog's released connectors, searchable, each with what the Teamspace already has of it.
 * Connect opens the new connection's editor in its card; saving returns to Connections, and an app's sign-in
 * returns there too. `?app=<connector>` opens one app's editor at once, as the suggestions on Connections do.
 */
export function BrowseApps() {
  const { teamSpaceId, connections, catalog, readError, canManage } = useConnections();
  const router = useRouter();
  const search = useSearchParams();
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(() => search.get("app") ?? "");
  const opened = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (adding && catalog.length) opened.current?.scrollIntoView({ block: "nearest" });
  }, [adding, catalog.length]);
  if (!teamSpaceId) return null;
  const back = studioRoutes.teamSpaceConnections(teamSpaceId);
  const needle = query.trim().toLowerCase();
  const shown = catalog
    .filter(
      (entry) =>
        !needle ||
        (entry.displayName || entry.connectorId).toLowerCase().includes(needle) ||
        entry.description.toLowerCase().includes(needle),
    )
    .sort((a, b) => (a.displayName || a.connectorId).localeCompare(b.displayName || b.connectorId));
  const have = (entry: ConnectorCatalogEntry) =>
    (connections ?? []).filter((connection) => connection.connectorId === entry.connectorId);
  return (
    <main className="min-h-0 flex-1 overflow-y-auto px-8 pt-6.5 pb-15 max-sm:px-4">
      <div className="mx-auto grid max-w-4xl gap-4">
        <Link className="justify-self-start text-[13px] text-ink-faint hover:text-ink" href={back}>
          ← Connections
        </Link>
        <h2 className="text-[22px] font-semibold tracking-[-0.01em] text-ink">Add an app</h2>
        <input
          aria-label="Search apps"
          className={SEARCH}
          placeholder={catalog.length ? `Search ${catalog.length} apps` : "Search apps"}
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        {readError && <Banner role="alert">{readError}</Banner>}
        {catalog.length === 0 && !readError ? (
          <p className="text-[13px] text-ink-faint" role="status">
            Loading the catalog…
          </p>
        ) : shown.length === 0 ? (
          <p className="text-[13px] text-ink-faint">No app matches “{query.trim()}”.</p>
        ) : (
          <ul className="grid gap-2.5 sm:grid-cols-2" aria-label="Apps">
            {shown.map((entry) => {
              const name = entry.displayName || entry.connectorId;
              const existing = have(entry);
              const needing = existing.filter((connection) => connectionStatus(connection).needsYou).length;
              const open = adding === entry.connectorId;
              return (
                <li
                  key={entry.connectorId}
                  ref={open ? (element) => void (opened.current = element) : undefined}
                  className={cx(
                    "grid gap-3 rounded-xl border bg-panel px-4 py-3",
                    open ? "border-accent-rule sm:col-span-2" : "border-line-soft",
                  )}
                  data-testid="team-space-catalog-app"
                >
                  <div className="flex items-start gap-3">
                    <AppLogo name={name} svg={entry.logoSvg} />
                    <div className="min-w-0 flex-1">
                      <p className="flex flex-wrap items-center gap-x-2 text-sm font-semibold text-ink">
                        {name}
                        {existing.length > 0 && (
                          <span className={cx("text-xs font-normal", needing ? "text-busy" : "text-ink-faint")}>
                            {needing
                              ? `${needing} need${needing === 1 ? "s" : ""} you`
                              : `${existing.length} connected`}
                          </span>
                        )}
                      </p>
                      <p className={cx("mt-0.5 text-xs/relaxed text-ink-faint", !open && "line-clamp-2")}>
                        {entry.description}
                      </p>
                    </div>
                    {canManage && !open && (
                      <Button
                        size="sm"
                        variant={existing.length ? "quiet" : "secondary"}
                        onClick={() => setAdding(entry.connectorId)}
                      >
                        {existing.length ? "Add another" : "Connect"}
                      </Button>
                    )}
                  </div>
                  {open && (
                    <LazyTeamSpaceConnectionEditor
                      teamSpaceId={teamSpaceId}
                      existing={null}
                      release={{
                        connectorId: entry.connectorId,
                        modulePath: entry.modulePath,
                        version: entry.version,
                        displayName: name,
                      }}
                      initialName={freeName(name, connections ?? [])}
                      returnPath={() => back}
                      onSaved={async () => router.push(back)}
                      onCancel={() => setAdding("")}
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </main>
  );
}
