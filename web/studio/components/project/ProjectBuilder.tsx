// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useMemo, type ReactNode } from "react";
import { FlowHostProvider } from "@/lib/flow/FlowHostProvider";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { ProcessDefinitionsProvider, useProcessDefinitions } from "@/lib/project/ProcessDefinitionsProvider";
import { initialTemplateSource, useExampleDefinitions } from "@/lib/project/initial-template-design";
import { useReleaseVersions } from "@/lib/project/use-release-versions";
import { canBuild, PROJECT_ROLES, projectRoleOf, type ProjectRole } from "@/lib/teamspace/project-roles";
import { useTeamSpace } from "@/lib/teamspace/TeamSpaceProvider";
import {
  useReadWaitingRuns,
  useWaitingRuns,
  waitingCount,
  waitingRunsTarget,
  WaitingRunsProvider,
} from "@/lib/project/WaitingRunsProvider";
import { liveStatusOf, previewStatusOf, setupNeedsYou } from "@/lib/project/setup/environment-status";
import { leaveFirstRun, ProjectStageProvider, useProjectStage } from "@/lib/project/stage/ProjectStageProvider";
import { preloadSwitchViews } from "@/components/flow/scope/LazyFlowViews";
import { availableSurfaces, useRevealedSurfaces } from "@/lib/project/stage/revealed-surfaces";
import { PreviewStartWord } from "@/components/build/PreviewStart";
import { ProjectShare } from "@/components/teamspace/ProjectShare";
import { draftWords, PreviewDraftProvider, usePreviewDraft } from "@/lib/project/setup/PreviewDraftProvider";
import { useNewestPublish } from "@/lib/project/setup/use-newest-publish";
import { configurationDrafts } from "@/lib/api/flow/draft/configuration-draft";
import { preparedSourceOf, readyPreparedSource } from "@/lib/session/session-status";
import { ComposerDraftProvider, useComposerDraft } from "@/lib/session/ComposerDraftProvider";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { useDesignShown } from "@/lib/session/design/use-design-shown";
import { useFlowDesign } from "@/lib/session/design/use-flow-design";
import { processName } from "@/lib/shared/format";
import { versionNumber } from "@/lib/shared/version/version";
import { studioRoutes, type ProjectTool } from "@/lib/shared/studio-routes";
import { openChatColumn } from "@/components/frame/frame-panes";
import { ProjectFrame } from "@/components/frame/ProjectFrame";
import { SURFACES, surfacesFor, TOOLS } from "@/components/frame/surface-registry";
import { useBuilderLocation } from "@/components/frame/use-builder-location";
import { useFrameLinks } from "@/components/frame/use-frame-links";
import { PublishButton } from "@/components/publishing/publish/PublishButton";
import { SessionChatStatus } from "@/components/session/SessionChatStatus";
import { SessionConversation } from "@/components/session/SessionConversation";
import { ElementPicker } from "@/components/session/composer/ElementPicker";
import { usePaletteEntries } from "@/components/shell/palette/use-palette-entries";
import { Badge } from "@/components/ui/badge/Badge";
import { NotReady } from "@/components/ui/NotReady";
import { Button } from "@/components/ui/Button";

/**
 * The Builder around a Project's surfaces: the frame with the Project's chat, the Preview | Live
 * switch with each environment's status and how far Live is behind, Publish, and the runs waiting
 * for a person, read once for the Inbox tab's count and the project map's cards.
 * The Builder's layout keeps it mounted while the surface below it changes.
 *
 * It follows the Project's stage (`useProjectStage`): a Project created in this browser opens on its
 * first conversation, the chat alone, and switches once to the standard frame on the Flow tab. There,
 * for those who build, a tab, Publish and Preview's status appear only once they have something to show.
 *
 * The person's role in the Project, which the server returns with it, decides what the frame shows (the
 * mock's role lens may set it instead): an operator sees Live only, with no chat and none of the Preview's
 * reads. The URL only selects what to show; it never decides access or editing.
 */
export function ProjectBuilder(props: BuilderProps) {
  const owner = useProjectResource();
  const project = owner?.snapshot;
  const role = props.role ?? (project ? projectRoleOf(project.callerRole) : null);
  const sessionResource = useSessionResource();
  const resource = sessionResource?.resource;
  const { environment, surface } = useBuilderLocation();
  const live = project?.live && !project.live.isRemoving ? project.live : null;
  const liveOnly = role !== null && PROJECT_ROLES[role].liveOnly;
  const shown = liveOnly ? "live" : live ? environment : "preview";
  const preview = liveOnly ? null : resource?.preview;
  const prepared = preview?.preparedSource;
  // The Preview's Setup is read for the source prepared for the pushed head, again for each new one.
  const ready = readyPreparedSource(preparedSourceOf(resource), resource?.headCommit);
  // Applying moves what the unapplied changes are counted against, so the configuration is read again.
  const setupSource =
    ready && !liveOnly
      ? JSON.stringify([ready.definitionRevision, ready.workspaceRequestId, preview?.appliedConfiguration ?? null])
      : null;
  // Read again on every surface change, so acting on an Inbox item shows in the counts.
  const waiting = useReadWaitingRuns(waitingRunsTarget(props.projectId, shown, live, preview), surface);
  if (role === null)
    return (
      <NotReady
        kind={owner?.error ? "error" : "placeholder"}
        heading="Opening the project"
        retry={Boolean(owner?.error)}
      >
        {owner?.error || "Reading the project and your role in it…"}
      </NotReady>
    );
  return (
    <ProcessDefinitionsProvider
      projectId={props.projectId}
      environment={shown}
      previewSource={
        prepared?.status === "PREVIEW_SOURCE_STATUS_READY"
          ? { commit: prepared.sourceCommit, definitionRevision: prepared.definitionRevision }
          : null
      }
      templateSource={liveOnly ? null : initialTemplateSource(project, resource)}
      liveReleaseId={live?.releaseId ?? null}
    >
      <WaitingRunsProvider runs={waiting}>
        <PreviewDraftProvider projectId={props.projectId} source={setupSource}>
          <ProjectStageProvider
            projectId={props.projectId}
            session={resource ?? null}
            drafting={Boolean(sessionResource?.requirementsDraft)}
          >
            <ComposerDraftProvider onPropose={openChatColumn}>
              <BuilderFrame {...props} role={role} />
              <ElementPicker />
            </ComposerDraftProvider>
          </ProjectStageProvider>
        </PreviewDraftProvider>
      </WaitingRunsProvider>
    </ProcessDefinitionsProvider>
  );
}

interface BuilderProps {
  projectId: string;
  /** The mock's role lens; production reads the role from the Project. */
  role?: ProjectRole;
  persistentQuery?: readonly string[];
  children: ReactNode;
}

function BuilderFrame({ projectId, role, persistentQuery, children }: BuilderProps & { role: ProjectRole }) {
  const project = useProjectResource()?.snapshot;
  const session = useSessionResource();
  const { surface, tool, environment, process, pinnedView } = useBuilderLocation();
  const definitions = useProcessDefinitions();
  const draft = useComposerDraft();
  const links = useFrameLinks(projectId, surface, persistentQuery);
  const live = project?.live && !project.live.isRemoving ? project.live : null;
  const liveOnly = PROJECT_ROLES[role].liveOnly;
  const shown = liveOnly ? "live" : live ? environment : "preview";
  // Only those who build see the versions ahead of Live.
  const versions = useReleaseVersions(
    canBuild(role) ? projectId : null,
    live?.releaseId ?? null,
    session?.resource?.headCommit,
  );
  const behind = versions.ahead.length;
  const previewDraft = usePreviewDraft();
  const newest = useNewestPublish(
    project ? projectId : null,
    `${live?.revision}:${project?.activeDeployment?.deploymentId}`,
  );
  const savingWords = previewDraft && !liveOnly ? draftWords(previewDraft.all) : null;
  const builds = canBuild(role);
  const stage = useProjectStage();
  const own = stage.hasOwnProcesses;
  // A link to another surface leaves the first conversation for good; the Flow's own URL keeps it.
  const conversation = builds && stage.stage === "conversation" && surface === "flow";
  useEffect(() => {
    if (builds && surface !== "flow") leaveFirstRun(projectId);
  }, [builds, projectId, surface]);
  useEffect(() => {
    if (conversation) preloadSwitchViews();
  }, [conversation]);
  // Until processes are built the bar names the stage in place of Preview | Live: Building from a recorded card
  // while the agent works on it or the Preview builds its built design or the agent's own head, else Shaping.
  const shaping = builds && !liveOnly && (stage.stage === "conversation" || stage.stage === "shaping");
  const agent = session?.resource?.agent?.description;
  const due = Boolean(agent?.confirmedRequirements?.spec?.designBuilt) || stage.buildsOwnHead;
  const building =
    Number(agent?.confirmedRequirements?.revision ?? 0) > 0 && (agent?.status !== "STATUS_WAITING_FOR_MESSAGE" || due);
  // Preview's status counts once processes are built from the agent's own push: before, it is the template's.
  const previewStatus =
    session && own
      ? previewStatusOf(session.resource?.preview ?? null, previewDraft?.configuration ?? null, stage.templateCommit)
      : undefined;
  const previewPrepared = session?.resource?.preview?.preparedSource;
  const waiting = useWaitingRuns();
  const waitingTotal = waiting ? [...waiting.values()].reduce((sum, count) => sum + count, 0) : 0;
  const tabs = useRevealedSurfaces(
    projectId,
    builds && !liveOnly && stage.known && !conversation ? availableSurfaces(stage, waitingTotal) : null,
    conversation ? null : surface,
  );
  // Those who build see the tabs with something to show; an operator sees every tab the role opens.
  const visible = builds && !liveOnly ? tabs.shown : undefined;
  // A Flow design shows on Preview to those who build, only before the first build of a Project that started from
  // the default template; its Flows join the picker, and the Flow tab's entry carries a marker until the Flow tab
  // shows it.
  const design = useFlowDesign(builds && shown === "preview" && stage.designFirst);
  const designShown = useDesignShown(projectId, design?.key ?? null).shown;
  const designedFlows = new Set(design?.body?.flows.map((flow) => flow.flowType) ?? []);
  // The template's own processes are not the person's: none show before the agent's own are built, except the
  // Flows of an example the Project started from, which the Flow tab draws while the head is the setup commit.
  const example = useExampleDefinitions(stage.drawsExample && shown === "preview" ? project?.example : null);
  const processes = own || liveOnly ? definitions.processes : example?.status === "ready" ? example.flows : [];
  const projectName = project?.name ?? "Project";
  const teamSpaceId = project?.teamSpaceId || null;
  // The sidebar shows the open Project's Teamspace, which the Project's URL does not name.
  const { follow } = useTeamSpace();
  useEffect(() => {
    if (teamSpaceId) follow(teamSpaceId);
  }, [follow, teamSpaceId]);
  usePaletteEntries([
    ...processes.map((item) => ({
      label: `${projectName} ▸ ${processName(item.flowType)}`,
      hint: "process",
      href: studioRoutes.projectSurface(projectId, "flow", { flowType: item.flowType }),
    })),
    ...surfacesFor(role)
      .filter((id) => !visible || visible.has(id))
      .map((id) => ({
        label: `${projectName} ▸ ${SURFACES[id].label}`,
        hint: "surface",
        href: links.surfaceHref(id),
      })),
    ...(builds && (!visible || visible.has("tools"))
      ? (Object.keys(TOOLS) as ProjectTool[]).map((id) => ({
          label: `${projectName} ▸ ${TOOLS[id].label}`,
          hint: "tool",
          href: links.toolHref(id),
        }))
      : []),
  ]);
  // "/" opens the chat and puts the cursor in the composer, unless the person is typing elsewhere.
  const requestFocus = draft?.requestFocus;
  useEffect(() => {
    if (!builds || !requestFocus) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented) return;
      const target = event.target as HTMLElement | null;
      if (target?.closest("input, textarea, select, [contenteditable='true']")) return;
      event.preventDefault();
      requestFocus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [builds, requestFocus]);
  const host = useMemo(
    () => ({ environment: shown, canIntervene: builds, askAgent: builds ? draft?.propose : undefined, teamSpaceId }),
    [builds, draft?.propose, shown, teamSpaceId],
  );
  return (
    <FlowHostProvider host={host}>
      <ProjectFrame
        projectId={projectId}
        teamSpaceId={teamSpaceId}
        projectName={projectName}
        surface={surface}
        tool={tool}
        pinnedView={pinnedView}
        environment={environment}
        role={role}
        liveOnly={liveOnly}
        hasLive={Boolean(live)}
        conversation={conversation}
        environmentSwitch={!shaping}
        stageBadge={
          shaping &&
          stage.stage === "shaping" && (
            <Badge tone={building ? "accent" : "neutral"} data-testid="project-stage">
              {building ? "Building" : "Shaping"}
            </Badge>
          )
        }
        surfaces={visible}
        fresh={visible && tabs.fresh}
        persistentQuery={persistentQuery}
        badges={{
          flow:
            design && !designShown && surface !== "flow" ? (
              <Badge tone="accent" title="The agent's Flow design is on the Flow tab" data-testid="design-marker">
                Design
              </Badge>
            ) : undefined,
          inbox:
            waitingTotal > 0 ? (
              <Badge tone="busy" title={`${waitingCount(waitingTotal)} waiting for a person`} data-testid="inbox-count">
                {waitingCount(waitingTotal)}
              </Badge>
            ) : undefined,
        }}
        processes={[
          ...processes.map((item) => ({
            flowType: item.flowType,
            name: processName(item.flowType),
            changed: shown === "preview" && definitions.changedSinceLive.has(item.flowType),
            designed: designedFlows.has(item.flowType),
          })),
          ...[...designedFlows]
            .filter((flowType) => !processes.some((item) => item.flowType === flowType))
            .map((flowType) => ({ flowType, name: processName(flowType), designed: true })),
        ]}
        process={process}
        setupNeedsYou={builds && !liveOnly && setupNeedsYou(previewDraft?.configuration ?? null)}
        switchWords={{
          previewStatus,
          previewDot: own || liveOnly,
          // A stopped Preview's word opens Start, on every tab, for those who build.
          previewWord:
            builds && previewStatus?.kind === "STOPPED" && previewStatus.word
              ? (selected) => (
                  <PreviewStartWord projectId={projectId} word={previewStatus.word ?? ""} selected={selected} />
                )
              : undefined,
          liveStatus: project
            ? liveStatusOf(
                project,
                newest,
                // Only those who build read the versions ahead of Live.
                !builds
                  ? ""
                  : behind > 0
                    ? `${versions.aheadExact ? behind : `more than ${behind}`} version${behind > 1 ? "s" : ""} behind Preview`
                    : "up to date",
              )
            : undefined,
          saving: savingWords && (
            <span
              role="status"
              className="shrink-0 text-[12px] whitespace-nowrap text-ink-faint"
              data-testid="draft-status"
            >
              {savingWords === "Not saved: retry" ? (
                <Button size="sm" variant="quiet" onClick={() => configurationDrafts.retry()}>
                  Not saved: retry
                </Button>
              ) : (
                savingWords
              )}
            </span>
          ),
        }}
        actions={
          <>
            {conversation && (
              // The way out when no card comes, such as after a chat-only first message or a failed turn.
              <Button size="sm" variant="quiet" onClick={() => leaveFirstRun(projectId)} data-testid="leave-first-run">
                Open the Flow tab
              </Button>
            )}
            {builds && teamSpaceId && (
              <ProjectShare
                projectId={projectId}
                projectName={projectName}
                teamSpaceId={teamSpaceId}
                owner={role === "owner"}
              />
            )}
            {builds && own && !conversation && (
              <PublishButton
                projectId={projectId}
                environment={shown}
                versions={versions}
                live={live}
                activeDeploymentId={project?.activeDeployment?.deploymentId ?? ""}
                previewSource={
                  previewPrepared?.status === "PREVIEW_SOURCE_STATUS_READY" ? previewPrepared.sourceCommit : ""
                }
                starts={session?.resource?.preview?.recentStarts ?? []}
                links={{
                  publishing: links.toolHref("publishing"),
                  setup: (anchor) => links.fixHref(shown, { to: "setup", anchor }),
                  preview: links.environmentHref("preview"),
                }}
              />
            )}
          </>
        }
        chat={
          <SessionConversation
            projectId={projectId}
            canManage={builds}
            live={
              shown === "live"
                ? {
                    commit: live?.releaseId ?? null,
                    version: live ? versionNumber(live.version) : null,
                    previewHref: links.environmentHref("preview"),
                  }
                : undefined
            }
          />
        }
        chatStatus={<SessionChatStatus />}
      >
        {children}
      </ProjectFrame>
    </FlowHostProvider>
  );
}
