// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";
import { nextPublishItemAnchor } from "@/lib/api/flow/draft/configuration-draft";
import { useQueryUpdate } from "@/lib/browser/use-query-update";
import { itemKeyOf, itemName, itemValueText, setupItems } from "@/lib/project/setup/configuration-items";
import { draftWords, usePreviewDraft } from "@/lib/project/setup/PreviewDraftProvider";
import { useNextPublish } from "@/lib/project/setup/use-next-publish";
import { useProjectResource } from "@/lib/project/ProjectResourceProvider";
import { useReleaseVersions } from "@/lib/project/use-release-versions";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { flowApiRoutes, studioRoutes, type ProjectEnvironment } from "@/lib/shared/studio-routes";
import { versionNumber } from "@/lib/shared/version/version";
import { PreviewSource } from "@/components/build/FlowSurface";
import { usePreviewCommands } from "@/components/build/use-preview-commands";
import { SetupChecklistSurface, type SetupNotes } from "@/components/flow/scope/SetupChecklistSurface";
import { LiveBanner } from "@/components/project/setup/LiveBanner";
import { SetupBar } from "@/components/project/setup/SetupBar";
import { openPublishPopover } from "@/components/publishing/publish/publish-opener";
import { ProblemPanel } from "@/components/ui/problem/ProblemPanel";

/** The tool panel, which Setup's table lays out by: its controls in the row, as chips, or stacked. */
function Page({ intro, children }: { intro: ReactNode; children: ReactNode }) {
  return (
    <div className="@container/tool grid max-w-[64rem] gap-3.5 px-6.5 pt-5.5 pb-10" data-setup-page>
      <header>
        <h2 className="text-lg font-semibold text-ink">Setup</h2>
        <p className="mt-1 max-w-240 text-[13px] text-ink-faint">{intro}</p>
      </header>
      {children}
    </div>
  );
}

/** The Live pointer while Live serves, and "Live V12" for the waiting dot's words. */
function useLive() {
  const project = useProjectResource()?.snapshot;
  const live = project?.live && !project.live.isRemoving ? project.live : null;
  return { live, name: live ? `Live ${versionNumber(live.version)}` : null };
}

/**
 * Setup on the Preview tab: the bar with Preview's line and the Next publish line, then one table with a
 * Preview column, which autosaves into Preview's draft that Apply to Preview runs, and a Next publish
 * column, Live's settings for the next publish, which the Live tab shows too.
 */
function PreviewTab({ projectId }: { projectId: string }) {
  const session = useSessionResource();
  const draft = usePreviewDraft();
  const commands = usePreviewCommands(projectId);
  const { live, name } = useLive();
  const applied = commands.preview?.appliedConfiguration?.configurationRevision ?? "";
  const next = useNextPublish(projectId, null, `${applied}:${live?.revision ?? ""}`);
  const configuration = draft?.configuration ?? null;
  const items = useMemo(() => (configuration ? setupItems(configuration) : null), [configuration]);
  const versions = useReleaseVersions(projectId, live?.releaseId ?? null, session?.resource?.headCommit);
  const { queryHref } = useQueryUpdate();
  const source = session?.resource?.preview?.preparedSource;
  const saving = draft ? draftWords(draft.all) : null;
  const newest = versions.ahead[0] ?? null;
  const [notes, setNotes] = useState<SetupNotes | null>(null);
  return (
    <Page
      intro={
        <>
          What this project&apos;s steps use from outside, and the settings they need. <b>Preview</b> is what Preview
          runs. <b>Next publish</b> is Live&apos;s settings, the same ones the Live tab shows: change them in either
          place and they reach Live when you publish. Changes save as you make them.
        </>
      }
    >
      <SetupBar
        configuration={commands.configuration}
        sourceReady={commands.sourceReady}
        preview={commands.preview}
        canApply={commands.allowed.apply}
        applying={commands.busy === "apply"}
        onApply={() => void commands.act("apply")}
        saving={saving === "Saving…" || saving === "Saved" ? saving : null}
        next={commands.sourceReady ? next.configuration : null}
        connectors={notes?.connectors ?? {}}
        warnings={notes?.warnings ?? []}
        nextError={next.error}
        waiting={next.waiting}
        ahead={newest ? versionNumber(newest.version, "the newest version") : null}
        onPublish={newest ? () => openPublishPopover({ mode: "version", commit: newest.sourceCommit }) : null}
        hrefOf={(anchor) => `${queryHref(() => undefined)}#${anchor}`}
        version={
          commands.preview?.status === "PREVIEW_STATUS_READY" && source
            ? versionNumber(source.version, "") || null
            : null
        }
      />
      {commands.problem && <ProblemPanel problem={commands.problem} actions={commands.remedyActions} />}
      {commands.sourceReady && (
        <SetupChecklistSurface
          previewPath={flowApiRoutes.previewConfiguration(projectId)}
          nextPath={next.basePath}
          version={JSON.stringify([
            session?.resource?.headCommit,
            source?.requestId,
            source?.status,
            applied,
            live?.revision,
          ])}
          items={{ preview: items, next: next.items }}
          live={name}
          addresses={{ preview: commands.preview?.url ?? "", next: live?.url ?? "" }}
          onNotes={setNotes}
        />
      )}
      <div className="-mx-4">
        <PreviewSource projectId={projectId} />
      </div>
      <p className="text-[13px] text-ink-faint">
        The project&apos;s repository, automatic builds and deletion are in{" "}
        <Link className="text-accent hover:underline" href={studioRoutes.project(projectId)}>
          Project details
        </Link>
        .
      </p>
    </Page>
  );
}

/**
 * Setup on the Live tab: the same Next publish controls in one column, checked against the version Live
 * runs, under the banner that says how many settings wait for a publish; then the connections a newer
 * version adds, set up from Preview, and what the next publish no longer uses.
 */
function LiveTab({ projectId }: { projectId: string }) {
  const { live, name } = useLive();
  const session = useSessionResource();
  const applied = session?.resource?.preview?.appliedConfiguration?.configurationRevision ?? "";
  const reread = `${applied}:${live?.revision ?? ""}`;
  const next = useNextPublish(live ? projectId : null, live?.releaseId ?? null, reread);
  const upcoming = useNextPublish(live ? projectId : null, null, reread);
  if (!live)
    return (
      <Page intro="Live's settings, once something is published.">
        <div className="max-w-240 rounded-[10px] border border-line-soft bg-panel px-5 py-7 text-center">
          <b className="text-[15px] text-ink">Not published yet</b>
          <p className="mt-1.5 text-[13.5px] text-ink-faint">
            Set Live up from Preview:{" "}
            <Link className="text-accent hover:underline" href={studioRoutes.setup(projectId, "preview")}>
              Setup
            </Link>{" "}
            has a Next publish column for what the first publish uses.
          </p>
        </div>
      </Page>
    );
  const current = new Set((next.configuration?.items ?? []).flatMap((item) => (item.key ? [itemKeyOf(item.key)] : [])));
  const arriving = (upcoming.configuration?.items ?? []).filter(
    (item) =>
      item.key?.kind === "KIND_CONNECTION" &&
      item.state !== "STATE_NO_LONGER_USED" &&
      !current.has(itemKeyOf(item.key)),
  );
  const gone = (upcoming.configuration?.items ?? []).filter((item) => item.state === "STATE_NO_LONGER_USED");
  return (
    <Page
      intro={
        <>
          Live&apos;s settings: the same ones as the <b>Next publish</b> column on Preview, so a change made in either
          place is the same change. A new key or sign-in on a connection reaches Live at once; anything else goes out
          with the next publish. A blue dot marks a setting that differs from what {name} runs now.
        </>
      }
    >
      <div>
        <LiveBanner
          version={versionNumber(live.version)}
          waiting={next.waiting}
          onPublish={() => openPublishPopover({ mode: "live" })}
        />
        <SetupChecklistSurface
          previewPath={null}
          nextPath={next.basePath}
          version={JSON.stringify([live.releaseId, live.revision, applied])}
          items={{ preview: null, next: next.items }}
          live={name}
          addresses={{ preview: "", next: live.url }}
          framed
        />
      </div>
      {arriving.length > 0 && (
        <section aria-label="Arrive with the next publish">
          <h3 className="mb-2 text-[11px] tracking-[0.07em] text-ink-faint uppercase">Arrive with the next publish</h3>
          <div className="flex flex-wrap gap-2">
            {arriving.map(
              (item) =>
                item.key && (
                  <Link
                    key={itemKeyOf(item.key)}
                    href={`${studioRoutes.setup(projectId, "preview")}#${nextPublishItemAnchor(itemKeyOf(item.key))}`}
                    className="rounded-md border border-line-soft bg-panel px-2.5 py-1 text-[12.5px] text-ink hover:border-line"
                  >
                    {item.key.connectionName}: set up from Preview ›
                  </Link>
                ),
            )}
          </div>
        </section>
      )}
      {gone.length > 0 && (
        <section aria-label="No longer used after the next publish">
          <h3 className="mb-2 text-[11px] tracking-[0.07em] text-ink-faint uppercase">
            No longer used after the next publish
          </h3>
          <ul className="grid gap-1 text-[12.5px] text-ink-faint">
            {gone.map(
              (item) =>
                item.key && (
                  <li key={itemKeyOf(item.key)}>
                    <span className="text-ink-dim">{itemName(item.key)}</span>
                    {item.liveValueJson && <> ({itemValueText(item.liveValueJson)})</>}
                  </li>
                ),
            )}
          </ul>
        </section>
      )}
    </Page>
  );
}

/** Tools, Setup on the environment the bar's tab selects. */
export function SetupPage({ projectId, environment }: { projectId: string; environment: ProjectEnvironment }) {
  return environment === "live" ? <LiveTab projectId={projectId} /> : <PreviewTab projectId={projectId} />;
}
