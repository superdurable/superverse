// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { createContext, useContext, useEffect, useSyncExternalStore, type ReactNode } from "react";
import {
  readStoredPreference,
  STORAGE_KEYS,
  subscribeStoredPreferences,
  writeStoredPreference,
} from "@/lib/browser/storage";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";

/**
 * How far a Project has come, as the Builder shows it: the first conversation (only the chat), shaping
 * (the requirements or a design are in, nothing of its own is built yet), built (processes built from the
 * agent's own push) and live (a published version runs).
 */
type ProjectStage = "conversation" | "shaping" | "built" | "live";

/**
 * What this browser recorded of a Project created here: its first conversation, then the one switch to
 * the standard Builder, then its own processes built. Each value only moves forward.
 */
const FIRST_RUN = ["conversation", "shaping", "built"] as const;
type FirstRun = (typeof FIRST_RUN)[number];

function firstRunOf(stored: string | null): FirstRun | null {
  return FIRST_RUN.find((value) => value === stored) ?? null;
}

/** How far a record is; a Project without one ranks lowest. */
function rank(value: FirstRun | null): number {
  return value === null ? -1 : FIRST_RUN.indexOf(value);
}

export interface ProjectStageState {
  stage: ProjectStage;
  /** The Project and, once it is ready, its Session were read, so the facts below are not guesses. */
  known: boolean;
  /** Processes built from the agent's own push exist, now or once before in this browser. */
  hasOwnProcesses: boolean;
  /** A published version runs on Live. */
  hasPublished: boolean;
  /** The agent recorded a version of what it builds, which carries the Flow design. */
  hasRequirements: boolean;
  /** The Preview serves, or served before it stopped, a page of the agent's own push. */
  previewServed: boolean;
  /** The commit the Project started from, the template's; empty while unknown. */
  templateCommit: string;
}

/** The Project's facts that decide its stage, from its snapshot and its Session's. */
function stageFacts(project: ProjectSnapshot__Output | null, session: SessionSnapshot__Output | null) {
  const templateCommit = project?.setup?.sourceCommit ?? "";
  // A commit is the agent's own when it is not the one the Project started from.
  const own = (commit: string | undefined) => Boolean(commit) && commit !== templateCommit;
  const preview = session?.preview;
  const prepared = preview?.preparedSource;
  const description = session?.agent?.description;
  const hasPublished = Boolean(project?.live && !project.live.isRemoving);
  return {
    templateCommit,
    hasPublished,
    builtNow:
      hasPublished ||
      (prepared?.status === "PREVIEW_SOURCE_STATUS_READY" && own(prepared.sourceCommit)) ||
      own(preview?.sourceCommit),
    previewServed: own(preview?.sourceCommit),
    hasRequirements: Number(description?.confirmedRequirements?.revision ?? 0) > 0,
  };
}

/**
 * Marks a Project just created in this browser as in its first conversation: the Builder shows only the
 * chat until the switch. A Project without this mark, such as one created elsewhere, opens the standard
 * Builder. Its tabs start with none shown, so every tab that appears later is marked new.
 */
export function beginFirstRun(projectId: string): void {
  // A replayed creation names a Project that may have moved on already.
  if (readStoredPreference(STORAGE_KEYS.projectStage(projectId)) !== null) return;
  writeStoredPreference(STORAGE_KEYS.projectStage(projectId), "conversation");
  writeStoredPreference(STORAGE_KEYS.projectTabs(projectId), JSON.stringify({ revealed: [], unvisited: [] }));
}

/**
 * Leaves the first conversation for good, as an explicit link to another surface or the first conversation's
 * "Open the Flow tab" does. Nothing changes for a Project past it or without a first run in this browser.
 */
export function leaveFirstRun(projectId: string): void {
  const key = STORAGE_KEYS.projectStage(projectId);
  if (readStoredPreference(key) === "conversation") writeStoredPreference(key, "shaping");
}

const ProjectStageContext = createContext<ProjectStageState | null>(null);

/**
 * The stage of the Builder's Project, from its snapshot and its Session's (`session`, null while the Project
 * has none). A Project created in this browser starts in its first conversation and switches once, never
 * back: when the agent starts writing the Session's first requirements card (`drafting`, its draft streams
 * in) or records it, or when processes are built from the agent's own push. Built stays built in this
 * browser once seen, so a rebuild of a newer push never hides what was there.
 */
export function ProjectStageProvider({
  projectId,
  session,
  drafting = false,
  children,
}: {
  projectId: string;
  session: SessionSnapshot__Output | null;
  drafting?: boolean;
  children: ReactNode;
}) {
  const key = STORAGE_KEYS.projectStage(projectId);
  const stored = firstRunOf(
    useSyncExternalStore(
      subscribeStoredPreferences,
      () => readStoredPreference(key),
      () => null,
    ),
  );
  const project = useProjectResource()?.snapshot ?? null;
  const facts = stageFacts(project, session?.projectId === projectId ? session : null);
  const known = project !== null && (project.status !== "PROJECT_STATUS_READY" || session !== null);
  const hasOwnProcesses = facts.builtNow || stored === "built";
  const switched = stored !== "conversation" || facts.hasRequirements || drafting || hasOwnProcesses;
  // Each move is recorded once and only forward.
  const reached: FirstRun | null = hasOwnProcesses ? "built" : switched && stored !== null ? "shaping" : null;
  useEffect(() => {
    if (reached && rank(reached) > rank(firstRunOf(readStoredPreference(key)))) writeStoredPreference(key, reached);
  }, [key, reached]);
  const value: ProjectStageState = {
    stage: !switched ? "conversation" : facts.hasPublished ? "live" : hasOwnProcesses ? "built" : "shaping",
    known,
    hasOwnProcesses,
    hasPublished: facts.hasPublished,
    hasRequirements: facts.hasRequirements,
    previewServed: facts.previewServed,
    templateCommit: facts.templateCommit,
  };
  return <ProjectStageContext.Provider value={value}>{children}</ProjectStageContext.Provider>;
}

/** The stage of the Builder's Project, for any part of the Builder; see `ProjectStageProvider`. */
/** The commit the Project started from, or "" outside a Builder, for code that also renders elsewhere. */
export function useTemplateCommit(): string {
  return useContext(ProjectStageContext)?.templateCommit ?? "";
}

export function useProjectStage(): ProjectStageState {
  const value = useContext(ProjectStageContext);
  if (!value) throw new Error("ProjectStageProvider is missing");
  return value;
}
