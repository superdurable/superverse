// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef } from "react";
import { errorMessage } from "@/lib/api/transport/api-error";
import { StudioLink as Link } from "@/lib/browser/StudioLink";
import { useFlowHost } from "@/lib/flow/FlowHostProvider";
import { useEvolve } from "@/lib/project/goals/EvolveProvider";
import { GOALS_FILE, goalsProblem } from "@/lib/project/goals/goal-values";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import type { ProjectEnvironment } from "@/lib/shared/studio-routes";
import { commitDetail, versionLabel } from "@/lib/shared/version/version";
import { useFrameLinks } from "@/components/frame/use-frame-links";
import { HistoryList } from "@/components/publishing/HistoryRows";
import { Notice } from "@/components/publishing/RequestProblem";
import { Button } from "@/components/ui/Button";
import { Glyph } from "@/components/ui/Glyph";
import { NotReady } from "@/components/ui/NotReady";
import { GoalProposals } from "./GoalProposals";
import { GoalReviews } from "./GoalReviews";
import { GoalRows } from "./GoalRows";

/** What Set goals puts in the composer. */
const SET_GOALS = "Help me set goals for this app";

/** What goals are for, to those who build (who can ask the agent) and to those who only read. */
const GOALS_FOR = {
  builder: "Tell the agent what this app is for, and every week its Live runs are measured against your goals.",
  reader:
    "Goals say what this app is for, and every week its Live runs are measured against them. Its builders set them.",
} as const;

/**
 * Goals: what the app is for, as the goals file of its Live version states it, measured against Live's runs. Without
 * goals it offers Set goals, which hands the agent the request; an invalid goals file shows its problems. The goals,
 * the agent's proposals and the reviews show on Live; Preview says where they are measured. Proposals stay on the tab
 * when a later Live version has no goals, so the chat card's Details and its decisions keep their row. The Builder
 * reads the measurement (`EvolveProvider`); opening the tab reads it again.
 */
export function GoalsSurface({ projectId, environment }: { projectId: string; environment: ProjectEnvironment }) {
  const { measurement, reload } = useEvolve();
  // The Builder's first read may still be on its way; only a measurement read before is read again.
  const opening = useRef(measurement.state === "reading");
  useEffect(() => {
    if (!opening.current) reload();
  }, [reload]);
  const { askAgent, canIntervene } = useFlowHost();
  const live = useProjectResource()?.snapshot?.live;
  const links = useFrameLinks(projectId, "goals");
  if (measurement.state === "reading")
    return (
      <NotReady kind="placeholder" heading="Reading goals">
        Reading the goals of the version that is Live…
      </NotReady>
    );
  if (measurement.state === "failed")
    return (
      <NotReady kind="error" heading="Goals did not load" retry>
        {errorMessage(measurement.error, "The goals could not be read.")}
      </NotReady>
    );
  const evolve = measurement.state === "ready" ? measurement.evolve : null;
  const goals = evolve?.goals;
  const invalid = goals?.status === "EVOLVE_GOALS_STATUS_INVALID";
  const valid = goals?.status === "EVOLVE_GOALS_STATUS_VALID" && goals.goals.length > 0;
  if (!evolve || (!invalid && !valid && evolve.proposals.length === 0))
    return (
      <NotReady
        kind="empty"
        heading="No goals yet"
        action={askAgent ? { label: "Set goals", onClick: () => askAgent("Goals", SET_GOALS) } : undefined}
      >
        {GOALS_FOR[askAgent ? "builder" : "reader"]}
        {environment === "preview" && ` Only Live runs count, and changes to ${GOALS_FILE} take effect when published.`}
      </NotReady>
    );
  // The goals are those of Live's build; its version names that commit.
  const liveVersion = live && goals && live.releaseId === goals.commit ? live.version : null;
  if (environment === "preview")
    return (
      <div className="min-h-0 flex-1 overflow-y-auto bg-well p-4 sm:p-6" data-testid="goals-preview">
        <div className="mx-auto max-w-170">
          <Notice>
            Goals are measured on Live runs; changes to <code className="font-mono">{GOALS_FILE}</code> take effect when
            published.{" "}
            {live && !live.isRemoving && (
              <Link href={links.environmentHref("live")} className="text-accent hover:underline">
                Show Live
              </Link>
            )}
          </Notice>
        </div>
      </div>
    );
  return (
    <div className="min-h-0 flex-1 overflow-y-auto bg-well p-4 sm:p-6" data-testid="goals-surface">
      <div className="mx-auto flex max-w-170 flex-col gap-6">
        <HistoryList
          label="Goals"
          heading="Goals"
          testId="goals"
          action={
            (valid || invalid) && (
              <span
                className="text-caption text-ink-faint"
                title={commitDetail(goals.commit)}
                data-testid="goals-source"
              >
                from <code className="font-mono">{GOALS_FILE}</code>{" "}
                {liveVersion ? (
                  <>
                    at <span className="text-ink-dim">{versionLabel(liveVersion)}</span>
                  </>
                ) : (
                  "of the Live version"
                )}
              </span>
            )
          }
        >
          {invalid ? (
            <GoalsProblems errors={goals.errors} askAgent={askAgent} />
          ) : valid ? (
            <GoalRows goals={goals.goals} reviews={evolve.reviews} />
          ) : (
            <NoGoals askAgent={askAgent} />
          )}
        </HistoryList>
        <GoalProposals projectId={projectId} evolve={evolve} />
        <GoalReviews projectId={projectId} evolve={evolve} canStart={canIntervene} onChange={reload} />
      </div>
    </div>
  );
}

/** No goals on Live while its proposals still show: what goals are for, and Set goals for those who build. */
function NoGoals({ askAgent }: { askAgent?: (about: string, text?: string) => void }) {
  return (
    <div className="grid justify-items-start gap-1 px-3 py-2.5" data-testid="goals-none">
      <p className="text-body font-medium text-ink">No goals yet</p>
      <p className="text-caption text-ink-dim">{GOALS_FOR[askAgent ? "builder" : "reader"]}</p>
      {askAgent && (
        <Button size="sm" className="mt-1" onClick={() => askAgent("Goals", SET_GOALS)}>
          Set goals
        </Button>
      )}
    </div>
  );
}

/** The parser's problems of an invalid goals file, each at its line, while reviews wait for a fixed file. */
function GoalsProblems({
  errors,
  askAgent,
}: {
  errors: readonly string[];
  askAgent?: (about: string, text?: string) => void;
}) {
  return (
    <div className="grid gap-2 px-3 py-2.5" data-testid="goals-problems">
      <p className="flex items-center gap-1.5 text-[13px] text-fail">
        <Glyph name="alert" className="size-4" />
        The goals file has problems. Reviews are paused until it is fixed and published.
      </p>
      <ul className="grid gap-0.5 text-caption text-ink-dim">
        {errors.map((error, index) => {
          const problem = goalsProblem(error);
          return (
            <li key={index}>
              {problem.line !== null && <span className="text-ink-faint">Line {problem.line}: </span>}
              <span className="font-mono">{problem.message}</span>
            </li>
          );
        })}
      </ul>
      {askAgent && (
        <Button
          size="sm"
          className="justify-self-start"
          onClick={() => askAgent("Goals", `Fix the problems of ${GOALS_FILE}:\n${errors.join("\n")}`)}
        >
          <Glyph name="spark" className="size-3.5" />
          Fix with the agent
        </Button>
      )}
    </div>
  );
}
