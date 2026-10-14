// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useEffect, useState } from "react";
import {
  listConnectorCatalog,
  listTeamSpaceConnections,
  type ConnectorCatalogEntry,
  type TeamSpaceConnection,
} from "@/lib/api/flow/team-space-connection";
import { errorMessage } from "@/lib/api/transport/api-error";
import { useAccount } from "@/lib/account/AccountProvider";
import { useProjectList } from "@/lib/project/use-project-list";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";

/**
 * What both Connections pages read: the TeamSpace's connections, the catalog with its logos, the Projects by
 * name, and whether the caller manages connections. Go decides that on every change; the pages only leave out
 * actions the caller's roles cannot take: a TeamSpace admin, the platform admin, or an owner or builder of one of
 * its Projects manages them, and a Project list still loading counts as managing.
 */
export function useConnections() {
  const { teamSpace } = useTeamSpace();
  const account = useAccount();
  const teamSpaceId = teamSpace?.teamSpaceId ?? null;
  const projects = useProjectList(teamSpaceId, "connections");
  const [connections, setConnections] = useState<TeamSpaceConnection[] | null>(null);
  const [catalog, setCatalog] = useState<ConnectorCatalogEntry[]>([]);
  const [readError, setReadError] = useState("");
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!teamSpaceId) return;
    const controller = new AbortController();
    void Promise.all([
      listTeamSpaceConnections(teamSpaceId, "", controller.signal),
      listConnectorCatalog(controller.signal),
    ]).then(
      ([listed, entries]) => {
        if (controller.signal.aborted) return;
        setConnections(listed);
        setCatalog(entries);
        setReadError("");
      },
      (reason: unknown) =>
        !controller.signal.aborted && setReadError(errorMessage(reason, "Could not read the connections")),
    );
    return () => controller.abort();
  }, [teamSpaceId, tick]);
  const reload = useCallback(() => setTick((value) => value + 1), []);
  const projectName = (projectId: string) =>
    projects.items.find((project) => project.projectId === projectId)?.name ?? projectId;
  const canManage =
    teamSpace?.callerRole === "TEAM_SPACE_ROLE_ADMIN" ||
    Boolean(account?.isAdmin) ||
    !projects.loaded ||
    Boolean(projects.nextPageToken) ||
    projects.items.some(
      (project) => project.callerRole === "PROJECT_ROLE_OWNER" || project.callerRole === "PROJECT_ROLE_BUILDER",
    );
  return { teamSpace, teamSpaceId, connections, catalog, readError, reload, projectName, canManage };
}
