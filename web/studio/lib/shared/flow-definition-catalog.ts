// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/** The fields of an analyzer FDG graph this catalog checks or reads. */
export interface DefinitionGraph {
  valid?: unknown;
  schemaVersion?: unknown;
  flow?: { name?: unknown };
  source?: { language?: unknown };
  v2?: unknown;
}

/** One Flow Definition as a Release, prepared source or template provides it. */
export interface DefinitionInput<Graph extends DefinitionGraph> {
  flowType: string;
  sourcePath: string;
  schemaVersion: string;
  digest: string;
  valid?: boolean;
  graph: Graph | null;
}

function requiredText(value: unknown, label: string): string {
  if (typeof value !== "string" || value.trim() === "") throw new Error(`Release Flow Definition ${label} is invalid`);
  return value;
}

function validatedDefinition<Graph extends DefinitionGraph>(value: DefinitionInput<Graph>) {
  const flowType = requiredText(value.flowType, "Flow Type");
  const sourcePath = requiredText(value.sourcePath, "source path");
  const digest = requiredText(value.digest, "digest");
  const { graph } = value;
  if (
    value.valid !== true ||
    value.schemaVersion !== "2.0" ||
    !/^sha256:[0-9a-f]{64}$/.test(digest) ||
    !graph ||
    graph.valid !== true ||
    graph.schemaVersion !== "2.0" ||
    graph.flow?.name !== flowType
  ) {
    throw new Error(`Release Flow Definition ${flowType} is inconsistent`);
  }
  return { flowType, sourcePath, graph };
}

/**
 * The Flow API's two catalog documents for one immutable definition set. Only
 * valid FDG 2.0 definitions with distinct Flow Types qualify; anything else
 * throws rather than showing a partial or substitute catalog.
 */
export function buildDefinitionDexCatalog<Graph extends DefinitionGraph>(
  definitionRevision: string,
  values: readonly DefinitionInput<Graph>[],
  source: string,
) {
  requiredText(definitionRevision, "revision");
  if (values.length === 0) throw new Error("Release contains no Flow Definitions");
  const definitions = values
    .map(validatedDefinition)
    .sort((left, right) => left.flowType.localeCompare(right.flowType));
  if (new Set(definitions.map((definition) => definition.flowType)).size !== definitions.length)
    throw new Error("Release contains duplicate Flow Types");
  return {
    definitions: {
      configured: true,
      source,
      definitionRevision,
      definitionCount: definitions.length,
      definitions: definitions.map(({ flowType, sourcePath, graph }) => ({
        id: flowType,
        file: sourcePath,
        schemaVersion: "2.0" as const,
        flowName: flowType,
        sourceLanguage: typeof graph.source?.language === "string" ? graph.source.language : "go",
        sourcePath,
        valid: true,
        graph,
      })),
    },
    catalog: {
      enabled: true,
      definitionRevision,
      flows: definitions.map(({ flowType, graph }) => ({ flowType, definition: graph.v2 })),
    },
  };
}
