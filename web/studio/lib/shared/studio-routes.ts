// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Every Studio page URL, Flow view link and Project-scoped Flow API base path is
// built here, so their shapes cannot drift between the pages that link to them.

type Query = Record<string, string | null | undefined>;

/** The surfaces of a Project's Builder, in tab order; each is the last segment of its URL. */
const PROJECT_SURFACES = ["flow", "runs", "inbox", "interface", "tools"] as const;
export type ProjectSurface = (typeof PROJECT_SURFACES)[number];

/** The tools that Tools lists, in list order; `?tool=` selects one. */
const PROJECT_TOOLS = ["publishing", "setup", "permissions"] as const;
export type ProjectTool = (typeof PROJECT_TOOLS)[number];

/** The tools that Setup replaced; their URLs and pinned tabs lead to Setup. */
const MOVED_TOOLS: Readonly<Record<string, ProjectTool>> = { connectors: "setup", configuration: "setup" };

/**
 * The version every surface shows: Preview, the latest, or Live. `?v=live` selects Live; a Project
 * opens on Preview.
 */
export type ProjectEnvironment = "preview" | "live";

export function environmentOf(v: string | null | undefined): ProjectEnvironment {
  return v === "live" ? "live" : "preview";
}

export function isProjectSurface(value: string): value is ProjectSurface {
  return (PROJECT_SURFACES as readonly string[]).includes(value);
}

export function isProjectTool(value: string | null | undefined): value is ProjectTool {
  return (PROJECT_TOOLS as readonly string[]).includes(value ?? "");
}

/** The tool a removed tool's name now leads to, such as Connectors to Setup; null for any other name. */
export function movedToolOf(value: string | null | undefined): ProjectTool | null {
  return value && Object.hasOwn(MOVED_TOOLS, value) ? MOVED_TOOLS[value] : null;
}

function withQuery(path: string, query: Query = {}): string {
  const search = new URLSearchParams(
    Object.entries(query).filter((entry): entry is [string, string] => Boolean(entry[1])),
  );
  return search.size === 0 ? path : `${path}?${search}`;
}

function teamSpace(teamSpaceId: string): string {
  return `/t/${encodeURIComponent(teamSpaceId)}`;
}

function project(projectId: string): string {
  return `/p/${encodeURIComponent(projectId)}`;
}

function projectSurface(projectId: string, surface: string, query?: Query): string {
  return withQuery(`${project(projectId)}/${encodeURIComponent(surface)}`, query);
}

function tool(projectId: string, name: ProjectTool, query?: Query): string {
  return projectSurface(projectId, "tools", { ...query, tool: name });
}

export const studioRoutes = {
  /** Opens the Teamspace last open in this browser, or offers to create the first one. */
  home: "/",
  login: "/login",
  register: "/register",
  /** A Teamspace's Home. */
  teamSpace,
  teamSpacePeople: (teamSpaceId: string) => `${teamSpace(teamSpaceId)}/people`,
  /** The Teamspace's connections, which its Projects' connection names select. */
  teamSpaceConnections: (teamSpaceId: string) => `${teamSpace(teamSpaceId)}/connections`,
  /** The catalog of apps a new Teamspace connection is made from. */
  teamSpaceConnectionCatalog: (teamSpaceId: string) => `${teamSpace(teamSpaceId)}/connections/browse`,
  /** The form that starts a new Project in the Teamspace. */
  projectStart: (teamSpaceId: string) => `${teamSpace(teamSpaceId)}/projects/start`,
  projectCreation: "/projects/new",
  project,
  projectSurface,
  tool,
  /** Where Preview is set up: Tools, Setup on Preview. */
  previewConfiguration: (projectId: string) => tool(projectId, "setup"),
  /**
   * Tools, Setup on `environment`; on Live, `version` names the commit whose next version it edits,
   * otherwise the page derives it from the server's state.
   */
  setup: (projectId: string, environment: ProjectEnvironment, version?: string) =>
    tool(projectId, "setup", environment === "live" ? { v: "live", version } : {}),
  publishingDeployment: (projectId: string, releaseId: string) =>
    tool(projectId, "publishing", { tab: "deploy", release: releaseId }),
};

/**
 * Where a URL of a surface that is not one of PROJECT_SURFACES leads: Build's Design is the Flow, its
 * Preview is the Interface and its Configuration a tool, and Publishing is a tool too. Any other
 * surface opens the Flow. The query, such as the environment or a Release, is kept.
 */
export function movedSurfaceHref(projectId: string, surface: string, query: Query): string {
  const { buildView, ...kept } = query;
  if (surface === "build" && buildView === "preview") return projectSurface(projectId, "interface", kept);
  if (surface === "build" && buildView === "configuration") return tool(projectId, "setup", kept);
  if (surface === "publishing") return tool(projectId, "publishing", kept);
  return projectSurface(projectId, "flow", kept);
}

function dexProject(projectId: string): string {
  return `/dex/projects/${encodeURIComponent(projectId)}`;
}

/** Base paths of the Project-scoped Flow API for each Studio surface. */
export const flowApiRoutes = {
  previewConfiguration: (projectId: string) => `${dexProject(projectId)}/configuration/preview`,
  liveConfiguration: (projectId: string, commit: string) =>
    `${dexProject(projectId)}/configuration/live/releases/${encodeURIComponent(commit)}`,
  /** Live's next-publish layer, checked against the Release of `commit`, or without one the Preview's source. */
  liveNextPublish: (projectId: string, commit: string | null) =>
    `${dexProject(projectId)}/configuration/live/next${commit ? `/releases/${encodeURIComponent(commit)}` : ""}`,
  previewRuntime: (projectId: string) => `${dexProject(projectId)}/environments/preview`,
  liveRuntime: (projectId: string, releaseId: string) =>
    `${dexProject(projectId)}/environments/live/releases/${encodeURIComponent(releaseId)}`,
  preparedSource: (projectId: string) => `${dexProject(projectId)}/source`,
};

/** Edits a copy of a URL query in place, such as `(query) => query.set("tab", "deploy")`. */
export type QueryChange = (query: URLSearchParams) => void;

/** `path` with a copy of the `current` query that `change` edited; parameters keep their order. */
export function withQueryChanges(path: string, current: URLSearchParams, change: QueryChange): string {
  const query = new URLSearchParams(current.toString());
  change(query);
  return query.size === 0 ? path : `${path}?${query}`;
}

/**
 * Where a Flow view can send the reader, as Studio project URLs. Runs and Inbox select a Flow type
 * and run through query parameters; Diagnostics is addressed by Flow ID (and optionally run) alone,
 * because a SubFlow or another run in the chain may belong to a different Flow type.
 */
export type FlowTarget =
  | { surface: "runs" | "inbox"; flowType?: string; flowId?: string }
  | { surface: "diagnostics"; flowId: string; runId?: string }
  | { surface: "configuration" };

/** The Studio URL of `target`, from the Project page at `pathname` with the query `search`. */
export function flowTargetHref(target: FlowTarget, pathname: string, search: URLSearchParams): string {
  const projectSegment = pathname.match(/^\/p\/([^/]+)\//)?.[1];
  if (!projectSegment) return pathname;
  const projectId = decodeURIComponent(projectSegment);
  if (target.surface === "configuration") {
    return search.get("v") === "live"
      ? studioRoutes.setup(projectId, "live")
      : studioRoutes.previewConfiguration(projectId);
  }
  // Other parameters, such as the selected environment, survive navigation.
  const surfacePath = projectSurface(projectId, target.surface === "inbox" ? "inbox" : "runs");
  return withQueryChanges(surfacePath, search, (next) => {
    for (const name of ["flowType", "flowId", "runId", "diagnostics"]) next.delete(name);
    if (target.surface === "diagnostics") {
      next.set("flowId", target.flowId);
      next.set("diagnostics", "true");
      if (target.runId) next.set("runId", target.runId);
    } else {
      if (target.flowType) next.set("flowType", target.flowType);
      if (target.flowId) next.set("flowId", target.flowId);
    }
  });
}
