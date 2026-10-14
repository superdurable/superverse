// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowApiContext__Output } from "@/lib/generated/protobuf/superverse/v2/FlowApiContext";
import type { FlowApiDocument__Output } from "@/lib/generated/protobuf/superverse/v2/FlowApiDocument";
import type { Caller } from "@/lib/server/caller-envelope";
import { callRpc } from "@/lib/server/grpc-client";
import { decimal64, httpError } from "@/lib/server/request-body";
import type { MethodOf, RpcInput } from "@/lib/shared/rpc-methods";
import { FlowApiJson } from "./flow-api-json";
import type { FlowApiPurpose } from "./flow-api-security";

type FlowApiMethod = MethodOf<
  "FlowRunService" | "FlowDiagnosticsService" | "ConnectorService" | "ApplicationEnvironmentService"
>;

/** One Flow API request after authorization: its verb, path and decoded parts. */
interface Operation {
  method: string;
  path: string[];
  pathKey: string;
  /** One query value; a repeated parameter is rejected. */
  query: (name: string) => string;
  /** The JSON body, admitting only the named fields. */
  body: (allowed: string[]) => FlowApiJson;
  /** The named RPC for this request's context and caller. */
  rpc: <M extends FlowApiMethod>(method: M, fields: Omit<RpcInput<M>, "context">) => Promise<FlowApiDocument__Output>;
}

// Every Flow API URL maps to one named generated RPC; no arbitrary transport crosses this boundary.
export async function dispatchFlowApiRequest(
  request: Request,
  path: string[],
  purpose: FlowApiPurpose,
  context: FlowApiContext__Output,
  caller: Caller,
  bodyText: string,
): Promise<FlowApiDocument__Output> {
  const search = new URL(request.url).searchParams;
  const operation: Operation = {
    method: request.method === "HEAD" ? "GET" : request.method,
    path,
    pathKey: path.join("/"),
    query: (name) => {
      const values = search.getAll(name);
      if (values.length > 1) throw httpError(400, `Repeated ${name}`);
      return values[0] ?? "";
    },
    body: (allowed) => new FlowApiJson(bodyText || "{}", allowed),
    rpc: async (method, fields) => {
      const input = { ...fields, context } as RpcInput<typeof method>;
      const { document } = (await callRpc(method, input, caller, request.signal)) as {
        document: FlowApiDocument__Output | null;
      };
      if (!document) throw httpError(502, "Invalid Flow API response");
      return document;
    },
  };
  const result = purpose === "runs" ? runsOperation(operation) : configurationOperation(operation);
  if (!result) throw httpError(404, "Unsupported Flow API operation");
  return result;
}

function runsOperation({ method, pathKey, query, body, rpc }: Operation): Promise<FlowApiDocument__Output> | null {
  const diagnostic = () => ({ flowId: query("flowId"), runId: query("runId") });
  switch (`${method} ${pathKey}`) {
    case "GET api/flow-definitions":
      return rpc("GetFlowDefinitions", {});
    case "GET api/v2/catalog":
      return rpc("GetFlowCatalog", {});
    case "POST api/v2/search": {
      const input = body(["flowType", "filters", "pageSize", "nextPageToken", "workQueueOnly"]);
      return rpc("SearchFlows", {
        flowType: input.string("flowType"),
        pageSize: input.integer("pageSize", 50),
        pageToken: input.string("nextPageToken"),
        workQueueOnly: input.boolean("workQueueOnly"),
        filters: input.objects("filters", ["field", "operator", "values"]).map((field) => ({
          field: field.string("field"),
          operator: field.string("operator"),
          valuesJson: field.raw("values", "[]"),
        })),
      });
    }
    case "GET api/v2/display":
      return rpc("GetFlowDisplay", { flowType: query("flowType"), flowId: query("flowId") });
    case "PATCH api/v2/display": {
      const input = body(["flowType", "flowId", "attributeKey", "value"]);
      return rpc("UpdateFlowDisplayField", {
        flowType: input.string("flowType"),
        flowId: input.string("flowId"),
        attributeKey: input.string("attributeKey"),
        valueJson: input.raw("value", "null"),
      });
    }
    case "POST api/v2/actions": {
      const input = body(["flowType", "flowId", "rpcName", "input", "attributeSnapshot"]);
      return rpc("InvokeFlowAction", {
        flowType: input.string("flowType"),
        flowId: input.string("flowId"),
        rpcName: input.string("rpcName"),
        inputJson: input.raw("input"),
        attributeSnapshotJson: input.raw("attributeSnapshot"),
      });
    }
    case "POST api/v2/start":
    case "POST api/v2/start/recover": {
      const input = body(["flowType", "flowId", "requestId", "targetRevision", "input"]);
      const start = {
        flowType: input.string("flowType"),
        flowId: input.string("flowId"),
        requestId: input.string("requestId"),
        targetRevision: input.string("targetRevision"),
        inputJson: input.raw("input"),
      };
      return pathKey.endsWith("/recover") ? rpc("RecoverFlowStart", start) : rpc("StartFlow", start);
    }
    case "GET api/flows/summary":
      return rpc("GetFlowSummary", diagnostic());
    case "GET api/flows/state":
      return rpc("GetFlowState", diagnostic());
    case "GET api/flows/history":
    case "GET api/flows/wait": {
      const wait = pathKey.endsWith("/wait");
      const after = query(wait ? "nextInternalEventId" : "startInternalEventId") || "0";
      const size = query("estimatePageSize") || "200";
      // Event IDs are non-negative int64 values.
      if (
        after.startsWith("-") ||
        decimal64("int64", after) === null ||
        !/^[1-9][0-9]*$/.test(size) ||
        Number(size) > 1000
      )
        throw httpError(400, "Invalid diagnostic history range");
      // WaitHistory ignores paging, so the long poll takes only its starting event.
      return wait
        ? rpc("WaitFlowHistory", { ...diagnostic(), afterEventId: after })
        : rpc("GetFlowHistory", {
            ...diagnostic(),
            afterEventId: after,
            pageSize: Number(size),
            pageToken: query("nextPageToken"),
          });
    }
    case "GET api/flows/stream":
      return rpc("ReadFlowStream", {
        flowId: query("flowId"),
        flowType: query("flowType"),
        streamName: query("streamName"),
        resumeToken: query("resumeToken"),
      });
    case "POST api/blobs/load": {
      const input = body(["flowId", "values"]);
      return rpc("LoadFlowBlobs", {
        flowId: input.string("flowId"),
        values: input
          .objects("values", ["id", "kind"])
          .map((value) => ({ id: value.string("id"), kind: value.string("kind") })),
      });
    }
    case "POST api/flows/stop": {
      const input = body(["flowId", "runId", "stopType", "reason"]);
      return rpc("StopFlow", {
        flowId: input.string("flowId"),
        runId: input.string("runId"),
        stopType: input.integer("stopType"),
        reason: input.string("reason"),
      });
    }
    case "DELETE api/flows/channels/messages": {
      const input = body(["flowId", "runId", "channelName", "messageId"]);
      return rpc("DeleteFlowChannelMessage", {
        flowId: input.string("flowId"),
        runId: input.string("runId"),
        channelName: input.string("channelName"),
        messageId: input.string("messageId"),
      });
    }
    case "POST api/flows/time-travel": {
      const input = body([
        "flowId",
        "runId",
        "timeTravelType",
        "reason",
        "stepType",
        "stepExecutionId",
        "historyEventTime",
        "stepMethod",
      ]);
      return rpc("TimeTravelFlow", {
        flowId: input.string("flowId"),
        runId: input.string("runId"),
        timeTravelType: input.integer("timeTravelType"),
        reason: input.string("reason"),
        stepType: input.string("stepType"),
        stepExecutionId: input.string("stepExecutionId"),
        historyEventTime: input.string("historyEventTime"),
        stepMethod: input.integer("stepMethod"),
      });
    }
    default:
      return null;
  }
}

function configurationOperation({
  method,
  path,
  pathKey,
  body,
  rpc,
}: Operation): Promise<FlowApiDocument__Output> | null {
  switch (`${method} ${pathKey}`) {
    case "GET api/v2/connector-connections":
      return rpc("ListConnections", {});
    case "POST api/v2/connector-usages/release-unused":
      return rpc("ReleaseUnusedConnections", {});
    case "GET api/v2/application-environment":
      return rpc("GetApplicationEnvironment", {});
    case "PUT api/v2/application-environment": {
      const input = body(["values", "secrets", "remove", "followPreview"]);
      return rpc("SaveApplicationEnvironment", {
        values: input.stringMap("values"),
        secrets: input.stringMap("secrets"),
        remove: input.strings("remove"),
        followPreview: input.strings("followPreview"),
      });
    }
    case "POST api/v2/connector-native-sessions": {
      const input = body(["connectorId", "connectionName"]);
      return rpc("StartConnectorUiSession", {
        connectorId: input.string("connectorId"),
        connectionName: input.string("connectionName"),
      });
    }
  }
  if (path[0] !== "api" || path[1] !== "v2") return null;
  // api/v2/{collection}/{connector}/{connection}/...
  const [, , collection, connectorId, connectionName] = path;
  const connection = { connectorId, connectionName };
  if (collection === "connector-selections" && path.length === 5 && method === "PUT") {
    const input = body(["teamSpaceConnectionId", "followPreview"]);
    return rpc("SelectTeamSpaceConnection", {
      ...connection,
      teamSpaceConnectionId: input.string("teamSpaceConnectionId"),
      followPreview: input.boolean("followPreview"),
    });
  }
  if (collection === "connector-ui-sessions" && path.length === 6 && path[4] === "commands" && method === "POST")
    return rpc("InvokeProviderCommand", {
      sessionNonce: path[3],
      commandId: path[5],
      parameters: body(["parameters"]).stringMap("parameters"),
    });
  if (collection === "connector-trigger-bindings" && path.length === 7 && method === "PUT") {
    const input = body(["configuration", "followPreview"]);
    return rpc("SaveTriggerBinding", {
      ...connection,
      triggerName: path[5],
      bindingName: path[6],
      configurationJson: input.raw("configuration"),
      followPreview: input.boolean("followPreview"),
    });
  }
  if (collection === "connector-use-configurations" && path.length === 8 && method === "PUT") {
    const input = body(["configuration", "followPreview"]);
    return rpc("SaveOperationConfiguration", {
      ...connection,
      operationId: path[5],
      flowType: path[6],
      stepType: path[7],
      configurationJson: input.raw("configuration"),
      followPreview: input.boolean("followPreview"),
    });
  }
  return null;
}
