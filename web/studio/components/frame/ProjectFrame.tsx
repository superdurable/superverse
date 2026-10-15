// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useRouter } from "next/navigation";
import { useRef, type ComponentProps, type ReactNode } from "react";
import type { ProjectEnvironment, ProjectSurface, ProjectTool } from "@/lib/shared/studio-routes";
import { canBuild, type ProjectRole } from "@/lib/teamspace/project-roles";
import { paneWidthStyle, useCollapsibleColumn } from "@/components/ui/pane/PaneResizeHandle";
import { EnvironmentSwitch } from "./bar/EnvironmentSwitch";
import { ProjectBar } from "./bar/ProjectBar";
import { PinnedTabs } from "./bar/PinnedTabs";
import { ProcessPicker, type PickerProcess } from "./bar/ProcessPicker";
import { ProjectMenu } from "./bar/ProjectMenu";
import { ChatColumn } from "./ChatColumn";
import { CHAT_PANE } from "./frame-panes";
import { NeedsYouMark, ToolsLayout } from "./ToolsLayout";
import { usePinnedTools } from "./use-pinned-tools";
import { SURFACES, surfacesFor, TOOLS } from "./surface-registry";
import { useFrameLinks } from "./use-frame-links";

type SwitchWords = Pick<
  ComponentProps<typeof EnvironmentSwitch>,
  "previewStatus" | "liveStatus" | "saving" | "previewWord"
>;

/**
 * The Builder: one 48px bar over the chat column and one surface. The chat belongs to the Project, so
 * it stays put on every surface, Process and version; only builders have it. The caller chooses the
 * surface's content and the chat; the frame draws everything around them.
 */
export function ProjectFrame({
  projectId,
  teamSpaceId,
  projectName,
  surface,
  tool,
  pinnedView = false,
  environment,
  role,
  liveOnly = false,
  hasLive,
  switchWords,
  processes,
  process,
  persistentQuery,
  badges,
  setupNeedsYou = false,
  actions,
  chat,
  chatStatus,
  children,
}: {
  projectId: string;
  /** The Project's TeamSpace, whose Projects the project switch lists; null while it is read. */
  teamSpaceId: string | null;
  projectName: string;
  surface: ProjectSurface;
  /** The open tool, on Tools. */
  tool: ProjectTool;
  /** The tool is open as a pinned tab. */
  pinnedView?: boolean;
  environment: ProjectEnvironment;
  role: ProjectRole;
  /** The role works on Live only: the switch holds Live alone. */
  liveOnly?: boolean;
  hasLive: boolean;
  switchWords?: SwitchWords;
  /** The Processes of the version shown, which the picker at the head of the tabs offers. */
  processes: readonly PickerProcess[];
  /** The picked Process (Flow Type), or null for all of them. */
  process: string | null;
  /** Query parameters every link of the frame keeps, such as the mock's role lens. */
  persistentQuery?: readonly string[];
  /** A count beside a tab, such as the runs waiting in the Inbox. */
  badges?: Partial<Record<ProjectSurface, ReactNode>>;
  /** Setup's Preview column lacks items or has changes to apply: Tools and its Setup entry carry a mark. */
  setupNeedsYou?: boolean;
  actions?: ReactNode;
  chat: ReactNode;
  chatStatus?: ReactNode;
  children: ReactNode;
}) {
  const frameRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const chatColumn = useCollapsibleColumn(CHAT_PANE);
  const links = useFrameLinks(projectId, surface, persistentQuery);
  const builds = canBuild(role);
  const router = useRouter();
  const pins = usePinnedTools();
  const pinTool = (next: ProjectTool) => {
    pins.pin(next);
    router.push(links.pinnedToolHref(next));
  };
  const unpinTool = (next: ProjectTool) => {
    pins.unpin(next);
    if (pinnedView && next === tool) router.push(links.toolHref(next));
  };
  // Without a Live deployment there is only Preview to show, whatever the link asked for, except to a role that
  // works on Live only.
  const shown = liveOnly ? "live" : hasLive ? environment : "preview";

  return (
    <div ref={frameRef} style={paneWidthStyle(chatColumn)} className="flex h-full min-h-0 w-full min-w-0 flex-col">
      <ProjectBar
        environment={shown}
        left={
          <>
            <ProjectMenu projectId={projectId} name={projectName} teamSpaceId={teamSpaceId} role={role} />
            <EnvironmentSwitch
              environment={shown}
              hasLive={hasLive}
              liveOnly={liveOnly}
              hrefOf={links.environmentHref}
              // Every fix opens a builder's surface or tool.
              fixHrefOf={builds ? links.fixHref : undefined}
              {...switchWords}
            />
          </>
        }
        tabs={surfacesFor(role).map((id) => ({ surface: id, label: SURFACES[id].label, glyph: SURFACES[id].glyph }))}
        current={pinnedView ? null : surface}
        hrefOf={links.surfaceHref}
        tabsAfter={
          builds ? (
            <PinnedTabs
              tools={(Object.keys(TOOLS) as ProjectTool[]).map((id) => ({ tool: id, ...TOOLS[id] }))}
              pinned={pins.pinned}
              current={pinnedView ? tool : null}
              hrefOf={links.pinnedToolHref}
              onPin={pinTool}
              onUnpin={unpinTool}
            />
          ) : undefined
        }
        tabsBefore={
          surface === "tools" ? undefined : (
            <ProcessPicker processes={processes} current={process} hrefOf={links.processHref} />
          )
        }
        chatOpen={builds && !chatColumn.isCollapsed}
        badges={
          setupNeedsYou
            ? { ...badges, tools: <NeedsYouMark title="Setup needs you" className="mt-3.5 -ml-0.5 self-start" /> }
            : badges
        }
        notes={setupNeedsYou ? { tools: "setup needs you" } : undefined}
        actions={actions}
      />
      <div ref={bodyRef} className="flex min-h-0 min-w-0 flex-1">
        {builds && (
          <ChatColumn column={chatColumn} targetRef={frameRef} measureRef={bodyRef} status={chatStatus}>
            {chat}
          </ChatColumn>
        )}
        <main
          aria-label={SURFACES[surface].label}
          className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-panel"
        >
          {surface === "tools" ? (
            <ToolsLayout
              current={tool}
              alone={pinnedView}
              hrefOf={links.toolHref}
              onPin={pinTool}
              needsYou={setupNeedsYou ? { setup: "Setup needs you" } : undefined}
            >
              {children}
            </ToolsLayout>
          ) : (
            children
          )}
        </main>
      </div>
    </div>
  );
}
