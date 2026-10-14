// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import {
  deleteTeamSpaceConnection,
  getTeamSpaceConnection,
  type ConnectorRelease,
  type TeamSpaceConnection,
} from "@/lib/api/flow/team-space-connection";
import { useSingleFlight } from "@/lib/browser/use-single-flight";
import { connectionRefusalText } from "@/lib/shared/problem/refusals";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { LazyTeamSpaceConnectionEditor } from "@/components/flow/scope/LazyFlowViews";
import { Banner } from "@/components/ui/Banner";
import { buttonStyle } from "@/components/ui/Button";
import { AppCard, type Editing } from "./AppCard";
import { AppLogo } from "./AppLogo";
import { SUGGESTED_APPS, appName, connectionStatus, freeName } from "./connection-words";
import { useConnections } from "./use-connections";

const SEARCH =
  "w-56 max-w-full rounded-lg border border-line bg-panel px-3 py-1.5 text-[13px] text-ink placeholder:text-ink-faint focus:border-accent focus:shadow-(--focus-ring) focus:outline-none";

/** The outcome an authorization brings back in the query, as the page says it. */
function oauthNotice(search: URLSearchParams): { text: string; failed: boolean } | null {
  const outcome = search.get("oauth");
  if (!outcome) return null;
  if (outcome === "success") return { text: "Signed in: the connection is ready.", failed: false };
  return {
    text: connectionRefusalText(search.get("reason") ?? "") ?? "The connection was not authorized.",
    failed: true,
  };
}

/**
 * Connections: the TeamSpace's apps, one card each with its connections, the ones that need someone first. Each
 * connection says which Projects use it on Preview and Live; it opens in place to edit, sign in again or delete.
 * Adding another connection of an app stays in its card; a new app comes from the catalog, behind "Add
 * connection" and a few suggestions at the bottom. A changed key or sign-in reaches every Project at its next
 * read; only an admin or a builder of every Project whose Live uses it may change it, which Go decides.
 */
export function ConnectionsPage() {
  const { teamSpace, teamSpaceId, connections, catalog, readError, reload, projectName, canManage } = useConnections();
  const router = useRouter();
  const pathname = usePathname();
  const search = useSearchParams();
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<Editing | null>(null);
  const [notice] = useState(() => oauthNotice(new URLSearchParams(search.toString())));
  const action = useSingleFlight<"open" | "delete">();
  useEffect(() => {
    if (search.get("oauth")) router.replace(pathname, { scroll: false });
  }, [search, pathname, router]);
  const apps = useMemo(() => {
    const byApp = new Map<string, TeamSpaceConnection[]>();
    for (const connection of connections ?? [])
      byApp.set(connection.connectorId, [...(byApp.get(connection.connectorId) ?? []), connection]);
    const needs = (items: TeamSpaceConnection[]) => items.filter((item) => connectionStatus(item).needsYou).length;
    return [...byApp]
      .map(([connectorId, items]) => ({
        connectorId,
        name: appName(connectorId, catalog),
        logoSvg: catalog.find((entry) => entry.connectorId === connectorId)?.logoSvg ?? "",
        connections: [...items].sort(
          (a, b) =>
            Number(connectionStatus(b).needsYou) - Number(connectionStatus(a).needsYou) || a.name.localeCompare(b.name),
        ),
        needsYou: needs(items),
      }))
      .sort((a, b) => Number(b.needsYou > 0) - Number(a.needsYou > 0) || a.name.localeCompare(b.name));
  }, [connections, catalog]);
  if (!teamSpace || !teamSpaceId) return null;
  const needle = query.trim().toLowerCase();
  const shown = needle
    ? apps
        .map((app) => ({
          ...app,
          connections: app.name.toLowerCase().includes(needle)
            ? app.connections
            : app.connections.filter((item) => item.name.toLowerCase().includes(needle)),
        }))
        .filter((app) => app.connections.length > 0)
    : apps;
  const total = connections?.length ?? 0;
  const needing = apps.reduce((sum, app) => sum + app.needsYou, 0);
  const suggestions = [
    ...SUGGESTED_APPS.flatMap((id) => catalog.filter((entry) => entry.connectorId === id)),
    ...catalog,
  ]
    .filter(
      (entry, index, all) => all.indexOf(entry) === index && !apps.some((app) => app.connectorId === entry.connectorId),
    )
    .slice(0, 6);
  const browse = studioRoutes.teamSpaceConnectionCatalog(teamSpaceId);
  const returnPath = () => pathname;
  const open = (connection: TeamSpaceConnection) =>
    void action.run("open", async (signal) => {
      const read = await getTeamSpaceConnection(teamSpaceId, connection.connectionId, signal);
      setEditing({ kind: "existing", ...read });
    });
  const remove = (connection: TeamSpaceConnection) =>
    void action.run("delete", async (signal) => {
      await deleteTeamSpaceConnection(connection, signal);
      setEditing(null);
      reload();
    });
  const saved = async () => {
    setEditing(null);
    reload();
  };
  /** A new connection's release: the app's catalog release, else the newest release one of its connections has. */
  const releaseOf = (connectorId: string, items: TeamSpaceConnection[]): ConnectorRelease => {
    const entry = catalog.find((item) => item.connectorId === connectorId);
    if (entry)
      return { connectorId, modulePath: entry.modulePath, version: entry.version, displayName: entry.displayName };
    const [latest] = [...items].sort((a, b) => b.manifestVersion.localeCompare(a.manifestVersion));
    return { connectorId, modulePath: latest.modulePath, version: latest.manifestVersion };
  };
  return (
    <main className="min-h-0 flex-1 overflow-y-auto px-8 pt-6.5 pb-15 max-sm:px-4">
      <div className="mx-auto grid max-w-4xl gap-5">
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-0 flex-1">
            <h2 className="text-[22px] font-semibold tracking-[-0.01em] text-ink">Connections</h2>
            <p className="mt-0.5 text-[13px] text-ink-faint" data-testid="team-space-connection-summary">
              {connections === null
                ? "Loading…"
                : `${apps.length} app${apps.length === 1 ? "" : "s"} · ${total} connection${total === 1 ? "" : "s"}${needing ? ` · ${needing} need${needing === 1 ? "s" : ""} you` : ""}`}
            </p>
          </div>
          <input
            aria-label="Search connections"
            className={SEARCH}
            placeholder="Search"
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          {canManage && (
            <Link className={buttonStyle("accent", "md")} href={browse}>
              + Add connection
            </Link>
          )}
        </div>
        <p className="-mt-2 max-w-2xl text-[13px]/relaxed text-ink-faint">
          The accounts and keys {teamSpace.name}&apos;s apps use. Set one up once here, or from a Project; each Project
          picks one for Preview, and Live uses the same unless the Project picks another.
        </p>
        {notice && (
          <Banner tone={notice.failed ? undefined : "busy"} role={notice.failed ? "alert" : "status"}>
            {notice.text}
          </Banner>
        )}
        {(readError || action.error) && <Banner role="alert">{action.error || readError}</Banner>}
        {!canManage && (
          <p className="rounded-lg border border-line-soft px-3 py-2.5 text-[13px] text-ink-dim" role="note">
            You can see these connections. A TeamSpace admin, or a builder of one of its Projects, can add and change
            them.
          </p>
        )}
        {connections === null && !readError ? (
          <p className="text-[13px] text-ink-faint" role="status">
            Loading connections…
          </p>
        ) : total === 0 ? (
          <p className="rounded-xl border border-dashed border-line px-5 py-6 text-[13px] text-ink-faint">
            No connections yet. Add an app below, or set one up from a Project&apos;s Connectors.
          </p>
        ) : shown.length === 0 ? (
          <p className="text-[13px] text-ink-faint">No app or connection matches “{query.trim()}”.</p>
        ) : (
          shown.map((app) => (
            <AppCard
              key={app.connectorId}
              name={app.name}
              logoSvg={app.logoSvg}
              connections={app.connections}
              projectName={projectName}
              canManage={canManage}
              editing={editing}
              busy={action.busy !== null}
              onOpen={open}
              onAdd={() => setEditing({ kind: "new", connectorId: app.connectorId })}
              onDelete={remove}
              editor={(read) => (
                <LazyTeamSpaceConnectionEditor
                  teamSpaceId={teamSpaceId}
                  existing={read}
                  release={{
                    connectorId: read.connection.connectorId,
                    modulePath: read.connection.modulePath,
                    version: read.connection.manifestVersion,
                    displayName: app.name,
                  }}
                  authMethodId={read.connection.authMethodId}
                  returnPath={returnPath}
                  onSaved={saved}
                  onCancel={() => setEditing(null)}
                />
              )}
              newEditor={() => (
                <LazyTeamSpaceConnectionEditor
                  teamSpaceId={teamSpaceId}
                  existing={null}
                  release={releaseOf(app.connectorId, app.connections)}
                  initialName={freeName(app.name, connections ?? [])}
                  returnPath={returnPath}
                  onSaved={saved}
                  onCancel={() => setEditing(null)}
                />
              )}
            />
          ))
        )}
        {canManage && catalog.length > 0 && (
          <section aria-label="Add more apps" className="mt-2 grid gap-3">
            <h3 className="text-[11px] font-semibold tracking-wider text-ink-faint uppercase">Add more apps</h3>
            <div className="grid grid-cols-[repeat(auto-fill,minmax(9.5rem,1fr))] gap-2">
              {suggestions.map((entry) => (
                <Link
                  key={entry.connectorId}
                  className="flex items-center gap-2.5 rounded-lg border border-line-soft bg-panel px-3 py-2 text-[13px] text-ink-dim hover:border-mark hover:text-ink"
                  href={`${browse}?app=${encodeURIComponent(entry.connectorId)}`}
                >
                  <AppLogo
                    name={entry.displayName || entry.connectorId}
                    svg={entry.logoSvg}
                    className="size-6 rounded-md"
                  />
                  <span className="truncate">{entry.displayName || entry.connectorId}</span>
                </Link>
              ))}
            </div>
            <Link className="justify-self-start text-[13px] text-accent hover:underline" href={browse}>
              Browse all {catalog.length} apps →
            </Link>
          </section>
        )}
      </div>
    </main>
  );
}
