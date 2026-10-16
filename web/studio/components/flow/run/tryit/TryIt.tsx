// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import Link from "next/link";
import { useCallback, useId, useState, type ReactNode } from "react";
import { useFlowCatalog } from "@/lib/flow/FlowCatalogProvider";
import { useFlowNavigation } from "@/lib/flow/FlowNavigationProvider";
import { processName } from "@/lib/shared/format";
import type { SetupItem } from "@/components/flow/connections/model/row-model";
import { StartForm } from "@/components/flow/run/start/StartForm";
import { useFlowTypeRoute } from "@/components/flow/run/use-flow-type-route";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { TryItChecklist, type ChecklistState } from "./TryItChecklist";

const SECTION_HEAD = "text-caption font-semibold tracking-wide text-ink-faint uppercase";

/** "Text summary" as it reads mid-sentence, "text summary"; an acronym such as "AI review" keeps its capitals. */
function midSentence(name: string): string {
  return /^[A-Z][a-z]/.test(name) ? name.charAt(0).toLowerCase() + name.slice(1) : name;
}

/**
 * Runs' list before Preview's version has any run: try the process on Preview. First exactly what the version
 * still needs, connected in place with Setup's own controls, while `autoApply` updates the Preview once the
 * edits settle; then the process's start form, filled from its defaults, whose run opens on the canvas.
 */
export function TryIt({
  configurationPath,
  items,
  version,
  address,
  setupHref,
  autoApply,
  onStarted,
}: {
  /** Preview's configuration host, which Setup's Preview column writes too. */
  configurationPath: string;
  /** The server's items of Preview's configuration with the gaps that name them; null while they are read. */
  items: Map<string, SetupItem> | null;
  /** Changes when something outside Try it changed the configuration, so it is read again. */
  version: string;
  /** Preview's address, for a connection tied to one. */
  address: string;
  /** Tools, Setup on Preview, for every setting the checklist leaves out. */
  setupHref: string;
  /** Setup's automatic apply with its one-line state, mounted while Try it shows. */
  autoApply?: ReactNode;
  /** A run Try it started; Try it then opens it under its own process, which reads the list again. */
  onStarted?: (flowID: string) => void;
}) {
  const route = useFlowTypeRoute("runs");
  const { catalog } = route;
  const { definitionUpdateKey, handleDefinitionError } = useFlowCatalog();
  const { navigate } = useFlowNavigation();
  const [checklist, setChecklist] = useState<ChecklistState>({ missing: 0 });
  const onState = useCallback((state: ChecklistState) => setChecklist(state), []);
  const [picked, setPicked] = useState("");
  const [notice, setNotice] = useState("");
  const headingId = useId();
  const startable = catalog.flows.filter((entry) => entry.definition.start);
  const entry = route.entry ?? startable.find((candidate) => candidate.flowType === picked) ?? startable[0];
  const start = entry?.definition.start;
  const target = catalog.startFlow;
  const process = entry ? processName(entry.flowType) : "";
  return (
    <section aria-labelledby={headingId} className="grid gap-5 pt-1 pb-4" data-testid="try-it">
      <header className="grid gap-1">
        <h3 id={headingId} className="text-title font-semibold text-ink">
          Try your process on Preview
        </h3>
        <p className="text-body text-ink-faint">Connect what it needs, then start a test run and follow it here.</p>
      </header>
      <section aria-label="What it needs" className="grid gap-1.5">
        <h4 className={SECTION_HEAD}>What it needs</h4>
        <TryItChecklist
          configurationPath={configurationPath}
          items={items}
          version={version}
          address={address}
          onState={onState}
        />
        {autoApply}
        <Link className="justify-self-start text-caption text-accent hover:underline" href={setupHref}>
          Every setting is in Setup ›
        </Link>
      </section>
      <section aria-label={`Run a test ${midSentence(process)}`} className="grid gap-2.5">
        <h4 className={SECTION_HEAD}>{entry ? `Run a test ${midSentence(process)}` : "Run a test"}</h4>
        {route.allProcesses && startable.length > 1 && (
          <Dropdown
            label="Process to try"
            size="md"
            value={entry?.flowType ?? null}
            onChange={setPicked}
            items={startable.map((candidate) => ({
              value: candidate.flowType,
              label: processName(candidate.flowType),
            }))}
          />
        )}
        {!entry || !start ? (
          <p className="text-body text-ink-faint">
            {entry ? `${process} does not start from a form.` : "No process of this version starts from a form."} Its
            runs appear here once something starts one.
          </p>
        ) : !target?.enabled ? (
          <p className="text-body text-ink-faint">
            Studio cannot start runs of this version. Its runs appear here once something starts one.
          </p>
        ) : (
          <>
            {checklist.missing > 0 && (
              <p className="text-caption text-ink-faint">
                Some of what it needs is not connected yet, so a test run may stop at the step that uses it.
              </p>
            )}
            <StartForm
              key={JSON.stringify([
                entry.flowType,
                catalog.definitionRevision,
                target.targetRevision,
                definitionUpdateKey,
              ])}
              definition={start}
              definitionRevision={catalog.definitionRevision}
              flowType={entry.flowType}
              target={target}
              layout="inline"
              onDefinitionChanged={handleDefinitionError}
              onStarted={(flowID, words) => {
                setNotice(words);
                onStarted?.(flowID);
                // Under its own process the canvas draws the run at once, and the list reads it.
                navigate({ surface: "runs", flowType: entry.flowType, flowId: flowID });
              }}
            />
          </>
        )}
        {notice && (
          <p className="text-caption text-pass" role="status">
            {notice}
          </p>
        )}
      </section>
    </section>
  );
}
