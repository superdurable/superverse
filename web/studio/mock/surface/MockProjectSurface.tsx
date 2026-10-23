// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { FlowSurface } from "@/components/build/FlowSurface";
import { InterfaceSurface } from "@/components/build/InterfaceSurface";
import { canOpenSurface, surfacesFor } from "@/components/frame/surface-registry";
import { canBuild, type ProjectRole } from "@/lib/teamspace/project-roles";
import { redirect } from "next/navigation";
import { ProjectToolContent } from "@/components/project/ProjectToolContent";
import { GoalsSurface } from "@/components/project/goals/GoalsSurface";
import {
  environmentOf,
  studioRoutes,
  type ProjectEnvironment,
  type ProjectSurface,
  type ProjectTool,
} from "@/lib/shared/studio-routes";
import {
  classifyDefinition,
  DEFINITION_STATES,
  FIXTURE_NAMES,
  inboxActions,
  isKnownFixture,
  loadFixtureDefinition,
  type DefinitionClassification,
} from "@/mock/fixture/flow-definitions";
import { FlowScope, WhilePreviewRuns } from "@/components/flow/scope/FlowScope";
import { InboxSurface, RunsSurface } from "@/components/flow/scope/FlowSurfaces";
import { TryItSurface } from "@/components/flow/scope/TryItSurface";
import { PreviewAutoApply } from "@/components/project/setup/PreviewAutoApply";
import { NotReady } from "@/components/ui/NotReady";
import { RecentEventsLog } from "@/components/project/activity/RecentEvents";
import { RecentEventsProvider } from "@/components/project/activity/RecentEventsProvider";
import { RecentEventsProblem } from "@/components/project/activity/RecentEventsProblem";
import { PreviewStartButton } from "@/components/build/PreviewStart";
import { resolveRole } from "./project-surfaces";
import { seedProposalFixture } from "@/mock/store/delivery/evolve-proposals";
import { seedGoalsFixture } from "@/mock/store/delivery/goal-measurement";
import { getProject, getStudioMockStore } from "@/mock/store/store";
import { seedPublishFixture } from "./publish-fixtures";
import { NARROW_SETUP_FIXTURE, seedSetupFixture } from "./setup-fixtures";

type MockProjectContentProps = {
  surface: ProjectSurface;
  tool: ProjectTool;
  projectId: string;
  flowHref: string;
  role: ProjectRole;
  environment: ProjectEnvironment;
  definition: unknown;
  classification: DefinitionClassification;
  loadFailed: boolean;
  /** Draws the tools narrow, as a narrow window would, so Setup shows its status chips. */
  narrow: boolean;
};

function MockProjectContent(props: MockProjectContentProps) {
  const { surface, projectId, role, classification, loadFailed } = props;
  // Tools, the Interface and Goals read the mock store, not the fixture definition.
  if (surface === "tools")
    return props.narrow ? (
      <div className="flex min-h-0 w-155 max-w-full flex-col border-r border-line-soft">
        <ProjectToolContent projectId={projectId} tool={props.tool} environment={props.environment} />
      </div>
    ) : (
      <ProjectToolContent projectId={projectId} tool={props.tool} environment={props.environment} />
    );
  if (surface === "interface") return <InterfaceSurface projectId={projectId} environment={props.environment} />;
  if (surface === "goals") return <GoalsSurface projectId={projectId} environment={props.environment} />;
  if (loadFailed) {
    return (
      <NotReady kind="error" heading="This did not load" detail={`process ${projectId} · ${surface}`}>
        The process definition could not be read. Nothing has changed and nothing is lost. Try again, and quote the line
        below if it keeps happening.
      </NotReady>
    );
  }
  if (classification.state === DEFINITION_STATES.UNSUPPORTED_VERSION && surface !== "flow") {
    return (
      <NotReady kind="unavailable" heading="Built with an older format" detail={classification.reason ?? undefined}>
        This process was described in an older format, which does not record who it asks or what they are shown.
        Everything it does still works, and the Flow can still draw it. Rebuilding the process refreshes the format.
      </NotReady>
    );
  }
  if (classification.state === DEFINITION_STATES.INVALID && surface !== "flow") {
    return (
      <NotReady
        kind="unavailable"
        heading="The description disagrees with the code"
        detail={classification.reason ?? undefined}
      >
        The process was read, but what it describes does not line up with the code behind it. Ask the agent for a fresh
        pass before relying on what any of these screens show.
      </NotReady>
    );
  }
  if (classification.state === DEFINITION_STATES.MISSING) {
    return (
      <NotReady kind="empty" heading="Nothing described yet" action={flowLink(role, props.flowHref)}>
        {canBuild(role)
          ? "No version of this process has been described, so there is nothing for these screens to show. Tell the agent what it should do and it will appear here."
          : "No version of this process has been described yet, so there is nothing for these screens to show. Whoever builds it has not started."}
      </NotReady>
    );
  }

  switch (surface) {
    case "flow":
      return (
        <FlowScope>
          <FlowSurface projectId={projectId} environment={props.environment} fixtureCatalog />
        </FlowScope>
      );
    case "inbox":
      return props.environment === "live" ? (
        <MockInboxSurface {...props} />
      ) : (
        <WhilePreviewRuns start={<PreviewStartButton projectId={projectId} />}>
          <MockInboxSurface {...props} />
        </WhilePreviewRuns>
      );
    case "runs": {
      // The fixture's runs show only while the mock Preview runs, as the running Preview's own do.
      const runs = (
        <FlowScope>
          <RunsSurface
            events={{ notice: <RecentEventsProblem />, log: <RecentEventsLog /> }}
            empty={
              props.environment === "live" ? undefined : (
                <TryItSurface projectId={projectId} autoApply={<PreviewAutoApply projectId={projectId} />} />
              )
            }
          />
        </FlowScope>
      );
      return (
        <RecentEventsProvider projectId={projectId} environment={props.environment === "live" ? "Live" : "Preview"}>
          {props.environment === "live" ? (
            runs
          ) : (
            <WhilePreviewRuns
              start={
                <>
                  <RecentEventsProblem />
                  <PreviewStartButton projectId={projectId} />
                </>
              }
            >
              {runs}
            </WhilePreviewRuns>
          )}
        </RecentEventsProvider>
      );
    }
  }
}

function flowLink(role: ProjectRole, flowHref: string) {
  return canBuild(role) ? { label: "Open the Flow", href: flowHref } : undefined;
}

function MockInboxSurface({ definition, role, flowHref }: MockProjectContentProps) {
  if (inboxActions(definition).length === 0) {
    return (
      <NotReady kind="empty" heading="Nobody is asked to do anything" action={flowLink(role, flowHref)}>
        This process runs start to finish without waiting for a person, so no work will ever appear here. If it should
        pause for approval, tell the agent.
      </NotReady>
    );
  }
  return (
    <FlowScope>
      <InboxSurface />
    </FlowScope>
  );
}

// Fixture loading and prototype roles belong exclusively to this mock assembly. The Builder's layout
// draws the frame (MockFrame); this page draws the surface.
export async function MockProjectSurface({
  projectId,
  surface,
  tool,
  query,
}: {
  projectId: string;
  surface: ProjectSurface;
  tool: ProjectTool;
  query: Record<string, string | undefined>;
}) {
  const failures = Number(query.configurationReadFailures);
  if (Number.isInteger(failures) && failures >= 1 && failures <= 4) {
    const project = getProject(getStudioMockStore(), projectId);
    if (project && project.configurationReadFailures === undefined) project.configurationReadFailures = failures;
  }
  seedSetupFixture(getStudioMockStore(), projectId, query.setupFixture);
  seedPublishFixture(getStudioMockStore(), projectId, query.publishFixture);
  seedGoalsFixture(getStudioMockStore(), projectId, query.goalsFixture);
  seedProposalFixture(getStudioMockStore(), projectId, query.proposalFixture);
  // Without a lens the page is a builder's; the frame shows the Project's own role.
  const role = resolveRole(query.role) ?? "builder";
  // A role lens that cannot open the surface lands on the first one it can.
  if (!canOpenSurface(role, surface)) redirect(studioRoutes.projectSurface(projectId, surfacesFor(role)[0], query));
  const fixture = query.fixture && isKnownFixture(query.fixture) ? query.fixture : null;
  const { definition, failed } = await loadFixtureDefinition(fixture ?? FIXTURE_NAMES.PROJECT);
  return (
    <MockProjectContent
      surface={surface}
      tool={tool}
      projectId={projectId}
      flowHref={studioRoutes.projectSurface(projectId, "flow", { role, fixture })}
      role={role}
      environment={environmentOf(query.v)}
      definition={definition}
      classification={classifyDefinition(definition)}
      loadFailed={failed}
      narrow={query.setupFixture === NARROW_SETUP_FIXTURE}
    />
  );
}
