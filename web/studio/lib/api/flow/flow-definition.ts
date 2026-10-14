// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * The Flow Definition Graph wire format emitted by `dexcli visualize --json`, including the Flow v2
 * block that declares indexed Attributes, Summary, Display, Actions and the Start input.
 *
 * `kind`, `wait.type` and `decision.type` are open strings because the schema declares them as
 * bare strings and real graphs carry values, such as the wait type `failure`, beyond the documented
 * enums.
 */

export interface SourceSpan {
  startLine: number;
  startColumn: number;
  endLine: number;
  endColumn: number;
}

export type FlowDefinitionConditionKind = "channel" | "timer" | "subflow" | "unknown";

interface FlowDefinitionCondition {
  kind: FlowDefinitionConditionKind;
  label: string;
  resourceId?: string;
  subFlowId?: string;
  expression?: string;
  span?: SourceSpan;
}

export interface FlowDefinitionNode {
  id: string;
  kind: string;
  name: string;
  parentId?: string;
  condition?: string;
  /** 'execute' | 'wait_for' | 'wait_for+execute' | 'rpc' | 'timeout': a `+`-joined compound. */
  phase?: string;
  start?: boolean;
  external?: boolean;
  span?: SourceSpan;
  resource?: {
    valueType: string;
    map?: boolean;
  };
  wait?: {
    type: string;
    conditions: FlowDefinitionCondition[];
  };
  decision?: {
    type: string;
    checkedChannels?: string[];
    cancellations?: Array<{
      stepId: string;
      scope: "all" | "siblings";
    }>;
  };
  metadata?: Record<string, unknown>;
}

interface FlowDefinitionEdge {
  id: string;
  kind: string;
  from: string;
  to: string;
  label?: string;
  condition?: string;
  /** A string such as "×N", not a number. */
  multiplicity?: string;
  span?: SourceSpan;
  metadata?: Record<string, unknown>;
}

interface FlowDefinitionDiagnostic {
  severity: "warning" | "error";
  code: string;
  message: string;
  span?: SourceSpan;
}

export interface FlowDefinitionGraph {
  schemaVersion: "1.0" | "2.0";
  valid: boolean;
  source: {
    /** "design" marks Studio's expansion of an agent's Flow design, which is never analyzer output. */
    language: "go" | "python" | "design";
    path: string;
  };
  flow: {
    name: string;
    startStepId?: string;
    span?: SourceSpan;
  };
  nodes: FlowDefinitionNode[];
  edges: FlowDefinitionEdge[];
  diagnostics: FlowDefinitionDiagnostic[];
  groups?: FlowDefinitionGroup[];
  v2?: FlowV2Definition;
}

interface FlowDefinitionGroup {
  id: string;
  label: string;
  stepIds: string[];
}

interface FlowV2IndexedAttribute {
  attributeKey: string;
  indexKey: string;
  indexType: "keyword" | "fulltext" | "keyword-array" | "int" | "double" | "bool" | "datetime";
  valueType: V2IndexedValueType;
  description: string;
}

export type V2ValueType =
  "string" | "string-array" | "int64" | "double" | "bool" | "datetime" | "json" | "object" | "array" | "attribute-map";

type V2IndexedValueType = "string" | "string-array" | "int64" | "double" | "bool" | "datetime";

type V2EditableValueType = Exclude<V2IndexedValueType, "string-array">;

export interface FlowV2Field {
  attributeKey: string;
  valueType: V2ValueType;
  editable: boolean;
  description: string;
  /** Named UI position this field takes in a row or drawer. */
  uiSlot?: string;
}

interface FlowV2View {
  rpcName: string;
  fields: FlowV2Field[];
}

interface FlowV2ActionInputField {
  fieldName: string;
  valueType: V2EditableValueType;
  source: "user" | "attribute";
  attributeKey?: string;
  /** Optional browser capture control for a user-provided string. */
  capture?: "qr-code";
  required: boolean;
  description: string;
}

export interface FlowV2Action {
  rpcName: string;
  label: string;
  /** Permission required to discover and authorize this Action. */
  requiredPermission: string;
  condition: {
    attributeKey: string;
    operator: "in";
    values: unknown[];
  };
  input: {
    kind: "none" | "object";
    fields?: FlowV2ActionInputField[];
  };
}

interface FlowV2StartInputEnumValue {
  name: string;
  value: string | number;
}

interface FlowV2StartInputField {
  name: string;
  required: boolean;
  schema: FlowV2StartInputSchema;
}

export interface FlowV2StartInputSchema {
  kind: "null" | "string" | "integer" | "number" | "boolean" | "object" | "array" | "map";
  nullable?: boolean;
  format?: "date-time";
  enumValues?: FlowV2StartInputEnumValue[];
  minimum?: string;
  maximum?: string;
  fields?: FlowV2StartInputField[];
  items?: FlowV2StartInputSchema;
  values?: FlowV2StartInputSchema;
  fixedLength?: number;
}

export interface FlowV2StartDefinition {
  stepType: string;
  input: FlowV2StartInputSchema;
}

export interface FlowV2Definition {
  indexedAttributes: FlowV2IndexedAttribute[];
  summary: FlowV2View;
  display: FlowV2View;
  actions: FlowV2Action[];
  start?: FlowV2StartDefinition;
}
