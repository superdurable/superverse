// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import appTemplateDesign from "@/lib/generated/template-design/app-template.json";
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
