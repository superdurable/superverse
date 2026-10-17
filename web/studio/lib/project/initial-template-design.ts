// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState } from "react";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { ProjectExample__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectExample";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import appTemplateDesign from "@/lib/generated/template-design/app-template.json";
import { EXAMPLE_DESIGNS, type ExampleDesign } from "@/lib/generated/template-design/example-designs";
import { buildDefinitionDexCatalog } from "@/lib/shared/flow-definition-catalog";
import type { FlowDefinitionGraph } from "@/lib/api/flow/flow-definition";
import type { CurrentSource } from "./ProcessDefinitionsProvider";

/**
 * The Design of a managed project's initial commit, before its source is
 * prepared. app-template.json is generated from app-template/ by
 * `go generate ./internal/dexai/app/apptemplate`, not an illustrative fixture. Only the
 * setup commit of a project created from this exact template revision shows it;
 * any later commit needs its own prepared source.
 */
export function initialTemplateDesign(
  project: ProjectSnapshot__Output | null | undefined,
  session: SessionSnapshot__Output | null | undefined,
) {
  if (
    !project ||
    !session ||
    project.projectId !== session.projectId ||
    project.templateRevision !== appTemplateDesign.templateRevision ||
    !project.setup?.sourceCommit ||
    project.setup.sourceCommit !== session.headCommit
  )
    return null;
  return buildDefinitionDexCatalog(
    `template:${appTemplateDesign.templateRevision}:${session.headCommit}`,
    appTemplateDesign.definitions,
    "initial-template",
  );
}

let templateSource: { revision: string; source: CurrentSource } | null = null;

/**
 * The template's initial design as the current source of the setup commit, for comparing a Flow design
 * with it; the same object while its revision holds, so its readers do not compare again.
 */
export function initialTemplateSource(
  project: ProjectSnapshot__Output | null | undefined,
  session: SessionSnapshot__Output | null | undefined,
): CurrentSource | null {
  const design = initialTemplateDesign(project, session);
  if (!design || !session) return null;
  const revision = design.definitions.definitionRevision;
  if (templateSource?.revision !== revision) {
    const graphs = design.definitions.definitions.map(
      (definition) => [definition.flowName, definition.graph as unknown as FlowDefinitionGraph] as const,
    );
    templateSource = { revision, source: { commit: session.headCommit, graphs: new Map(graphs) } };
  }
  return templateSource.source;
}

/** An example's Flow definitions as the Flow views read a catalog: the two documents of one definition set. */
type ExampleCatalog = ReturnType<typeof buildDefinitionDexCatalog<ExampleDesign["definitions"][number]["graph"]>>;

/** The Flow definitions of a Project's example: loading, drawn, failed to load, or not in this Studio build. */
export type ExampleDefinitions =
  | { status: "loading" }
  | { status: "ready"; design: ExampleCatalog; flows: readonly { flowType: string }[] }
  | { status: "failed"; error: string }
  | { status: "unavailable" };

/** Each example's catalog once imported, by example and tree, shared by every reader. */
const exampleCatalogs = new Map<string, { design: ExampleCatalog; flows: readonly { flowType: string }[] }>();

/**
 * The Flow definitions of the example a Project started from, at the tree it started from, which the Builder
 * draws while the Session's head is the setup commit and the Preview has built no processes: rendered from the
 * example's own source when this Studio was built, and imported on first use, once per example, only for a
 * Project that starts from it. They are unavailable when this build holds the example at another tree, as
 * during a rollout, and null without an example.
 */
export function useExampleDefinitions(example: ProjectExample__Output | null | undefined): ExampleDefinitions | null {
  const generated = example ? EXAMPLE_DESIGNS[example.exampleId] : undefined;
  const key = example && generated?.tree === example.tree ? `${example.exampleId}@${example.tree}` : null;
  // The import's outcome of a key: no error once it is imported, else why it failed.
  const [read, setRead] = useState<{ key: string; error?: string } | null>(null);
  useEffect(() => {
    if (key === null || !generated || exampleCatalogs.has(key)) return undefined;
    let current = true;
    // A definition set the catalog refuses fails as an import that did not load.
    generated
      .definitions()
      .then(({ default: design }) => {
        const catalog = buildDefinitionDexCatalog(`example:${design.templateRevision}`, design.definitions, "example");
        const flows = catalog.definitions.definitions.map((definition) => ({ flowType: definition.flowName }));
        exampleCatalogs.set(key, { design: catalog, flows });
      })
      .then(
        () => current && setRead({ key }),
        (reason: unknown) =>
          current && setRead({ key, error: errorMessage(reason, "The Flow definitions did not load") }),
      );
    return () => {
      current = false;
    };
  }, [generated, key]);
  if (!example) return null;
  if (key === null) return { status: "unavailable" };
  const ready = exampleCatalogs.get(key);
  if (ready) return { status: "ready", ...ready };
  if (read?.key === key && read.error !== undefined) return { status: "failed", error: read.error };
  return { status: "loading" };
}
