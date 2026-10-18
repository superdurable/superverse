// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { deleteProject } from "@/lib/api/project";
import { isRevisionConflict, newRequestId } from "@/lib/api/transport/api-error";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import { useResourceWatch } from "@/lib/project/use-resource-watch";
import { SessionProvider } from "@/lib/session/SessionProvider";
import { canBuild, projectRoleOf } from "@/lib/teamspace/project-roles";
import { enumText } from "@/lib/shared/protobuf";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { PreviewAutoDeploy } from "@/components/build/PreviewAutoDeploy";
import { WorkspaceDetails } from "@/components/session/SessionStatusIndicators";
import { Banner } from "@/components/ui/Banner";
import { Button, buttonStyle } from "@/components/ui/Button";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { Spinner } from "@/components/ui/Icons";
import { ProblemPanel, type RemedyActions } from "@/components/ui/problem/ProblemPanel";
import { errorProblem, failureProblem } from "@/lib/shared/problem/problem";

export function ProjectPage({ projectId }: { projectId: string }) {
  return <ProjectPageContent key={projectId} projectId={projectId} />;
}

/** A section's heading, as Tools and the Builder's lists label theirs. */
const SECTION = "mb-2 text-[11px] font-normal tracking-[0.07em] text-ink-faint uppercase";

/** Whether the caller builds the Project, whose Session holds the Preview's automatic builds. */
function canBuildIn(project: ProjectSnapshot__Output): boolean {
  const role = projectRoleOf(project.callerRole);
  return role !== null && canBuild(role);
}

/**
 * The Project's details: its status and repository, the Preview's automatic builds and the workspace
 * (for those who build, read from the Project's Session), and deletion. Its one Session, which ProjectFlow starts
 * once the Project is READY, opens in the Builder's chat.
 */
function ProjectPageContent({ projectId }: { projectId: string }) {
  const [project, setProject] = useState<ProjectSnapshot__Output | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [deletingProject, setDeletingProject] = useState(false);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const pageRequest = useRef<AbortController | null>(null);

  const deletionRequestId = useRef<string | null>(null);
  const retryingCleanup = project?.status === "PROJECT_STATUS_DELETING" && Boolean(project.failure);
  const removeProject = useCallback(async () => {
    const page = pageRequest.current;
    if (!page || page.signal.aborted || !project || deletingProject) return;
    setDeletingProject(true);
    setError(null);
    deletionRequestId.current ??= newRequestId();
    try {
      const accepted = await deleteProject(
        { projectId, requestId: deletionRequestId.current, expectedRevision: project.revision },
        page.signal,
      );
      if (page.signal.aborted || pageRequest.current !== page) return;
      setProject((current) => (current && BigInt(current.revision) > BigInt(accepted.revision) ? current : accepted));
      setConfirmingDelete(false);
    } catch (reason) {
      if (page.signal.aborted) return;
      // The page's Watch shows the Project as it is now; the next attempt is fenced on that.
      setError(
        isRevisionConflict(reason)
          ? Object.assign(
              new Error("The project changed meanwhile, so it was not deleted. Check it again before deleting."),
              { errorInfo: (reason as { errorInfo?: unknown }).errorInfo },
            )
          : reason,
      );
    } finally {
      if (!page.signal.aborted && pageRequest.current === page) setDeletingProject(false);
    }
  }, [projectId, project, deletingProject]);

  // Trying a deletion again asks for its confirmation again, against the Project as it is now.
  const retryDelete: RemedyActions = { FAILURE_REMEDY_APPLY_AGAIN: { onClick: () => setConfirmingDelete(true) } };

  useEffect(() => {
    const controller = new AbortController();
    pageRequest.current = controller;
    return () => {
      controller.abort();
      if (pageRequest.current === controller) pageRequest.current = null;
    };
  }, [projectId]);

  const acceptProject = useCallback((current: ProjectSnapshot__Output) => {
    const page = pageRequest.current;
    if (!page || page.signal.aborted) return;
    setProject((previous) => (previous && BigInt(previous.revision) > BigInt(current.revision) ? previous : current));
  }, []);
  const watch = useResourceWatch("project", projectId, projectId, acceptProject);

  const deletable = project !== null && project.status !== "PROJECT_STATUS_DELETED";
  const builds = project !== null && canBuildIn(project);
  return (
    <main className="flex min-h-0 min-w-0 flex-1 justify-center-safe overflow-y-auto p-8 max-sm:p-4">
      <div className="w-full max-w-180">
        <Eyebrow>Project</Eyebrow>
        <h1 className="mt-2 text-3xl font-semibold tracking-tight">{project?.name ?? "Loading project…"}</h1>
        {project && (
          <div className="mt-4 rounded-[10px] border border-line-soft bg-panel p-4 text-[13px]">
            <dl className="grid grid-cols-[7rem_minmax(0,1fr)] gap-x-3 gap-y-1.5">
              <dt className="text-ink-faint">Status</dt>
              <dd className="flex items-center gap-2 text-ink">
                {project.status === "PROJECT_STATUS_CREATING" && <Spinner className="h-3.5 w-3.5" />}
                <span>{enumText(project.status, "PROJECT_STATUS").toLowerCase()}</span>
              </dd>
              <dt className="text-ink-faint">Repository</dt>
              <dd className="font-mono text-xs break-all text-ink">
                {project.repository?.fullName || "Resolving repository…"}
              </dd>
            </dl>
            {(project.status === "PROJECT_STATUS_READY" || project.status === "PROJECT_STATUS_CREATING") && (
              <div className="mt-3 flex flex-wrap items-center gap-4">
                <Link href={studioRoutes.projectSurface(projectId, "flow")} className={buttonStyle("primary", "sm")}>
                  Open the project
                </Link>
                {project.status === "PROJECT_STATUS_READY" && (
                  <Link
                    href={studioRoutes.tool(projectId, "publishing")}
                    className="text-[13px] text-accent hover:underline"
                  >
                    Publishing
                  </Link>
                )}
              </div>
            )}
          </div>
        )}
        {builds && project.status === "PROJECT_STATUS_READY" && (
          <SessionProvider projectId={projectId} active>
            <section aria-label="Automatic builds" className="mt-6">
              <h2 className={SECTION}>Preview</h2>
              <div className="rounded-[10px] border border-line-soft bg-panel p-4">
                <PreviewAutoDeploy projectId={projectId} />
              </div>
            </section>
            <WorkspaceDetails headingClassName={SECTION} />
          </SessionProvider>
        )}
        {deletable && (
          <section aria-label="Delete the project" className="mt-6">
            <h2 className={SECTION}>Danger zone</h2>
            <div className="flex flex-wrap items-center gap-3 rounded-[10px] border border-line-soft bg-panel p-4 text-[13px]">
              <p className="min-w-0 flex-1 text-ink-dim">
                {confirmingDelete
                  ? retryingCleanup && project.failure?.isRecoverable === false
                    ? "After addressing the failure, retry resource cleanup?"
                    : "Clean up all managed resources?"
                  : "Deleting stops Live and Preview and cleans up every managed resource; the repository is archived."}
              </p>
              {confirmingDelete ? (
                <div className="flex flex-wrap items-center justify-end gap-2">
                  <Button size="sm" disabled={deletingProject} onClick={() => setConfirmingDelete(false)}>
                    Cancel
                  </Button>
                  <Button variant="danger" size="sm" disabled={deletingProject} onClick={() => void removeProject()}>
                    {deletingProject
                      ? "Deleting…"
                      : retryingCleanup
                        ? "Confirm retry cleanup"
                        : "Confirm delete project"}
                  </Button>
                </div>
              ) : (
                <Button
                  variant="danger"
                  size="sm"
                  disabled={deletingProject || (project.status === "PROJECT_STATUS_DELETING" && !retryingCleanup)}
                  onClick={() => {
                    if (retryingCleanup) deletionRequestId.current = null;
                    setConfirmingDelete(true);
                  }}
                >
                  {deletingProject
                    ? "Deleting…"
                    : retryingCleanup
                      ? "Retry cleanup"
                      : project.status === "PROJECT_STATUS_DELETING"
                        ? "Deleting…"
                        : "Delete project"}
                </Button>
              )}
            </div>
          </section>
        )}
        {project?.setup?.status === "SETUP_STATUS_SETUP_FAILED" && !project.setup.failure && (
          <Banner className="mt-4">
            Project setup failed. Delete this project to clean up its resources, then create a new project.
          </Banner>
        )}
        {project?.sessionStartFailure && (
          <ProblemPanel
            className="mt-4"
            problem={failureProblem(project.sessionStartFailure, { operation: "Starting the Project's Agent" })}
          />
        )}
        {project?.failure ? (
          <ProblemPanel
            className="mt-4"
            problem={failureProblem(project.failure, {
              operation: project.status === "PROJECT_STATUS_DELETING" ? "Deleting the Project" : "Creating the Project",
            })}
            actions={retryDelete}
          />
        ) : (
          project?.setup?.failure && (
            <ProblemPanel
              className="mt-4"
              problem={failureProblem(project.setup.failure, { operation: "Setting up the Project" })}
              actions={retryDelete}
            />
          )
        )}
        {watch?.disconnected && (
          <p className="mt-4 text-sm text-ink-faint" aria-live="polite">
            Project updates disconnected. Reconnecting…
          </p>
        )}

        {project?.status === "PROJECT_STATUS_CREATING" && (
          <p className="mt-6 text-[15px] text-ink-dim">Creating the private repository from the trusted template…</p>
        )}

        {Boolean(error || watch?.error) && (
          <ProblemPanel
            className="mt-5"
            problem={errorProblem(error || watch?.error, {
              operation: error ? "Deleting the Project" : "Reading the Project",
            })}
            actions={retryDelete}
          />
        )}
      </div>
    </main>
  );
}
