// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { SessionExecutorStatus } from "./executor/SessionExecutorStatus";
import { RemoteAgentDialog } from "./remoteagent/RemoteAgentDialog";
import { SessionConnectionIndicator, SessionDetails } from "./SessionStatusIndicators";

/**
 * Beside the chat's heading: a dot while live updates reconnect, the agent that works in the chat, and
 * More details with the live updates, the workspace's time and its startup.
 */
export function SessionChatStatus({ projectId, canManage }: { projectId: string; canManage: boolean }) {
  const session = useSessionResource();
  // Navigation or lost management permission discards acknowledgement and codes.
  return (
    <ChatStatus key={`${projectId}:${canManage}:${session?.status}`} projectId={projectId} canManage={canManage} />
  );
}

function ChatStatus({ projectId, canManage }: { projectId: string; canManage: boolean }) {
  const [connect, setConnect] = useState(false);
  const session = useSessionResource();
  const canConnect = canManage && session?.projectId === projectId && session.status === "SESSION_STATUS_ACTIVE";
  return (
    <div className="flex min-w-0 items-center gap-2">
      <SessionConnectionIndicator />
      <SessionExecutorStatus canManage={canConnect} onConnect={() => setConnect(true)} />
      <SessionDetails />
      {connect && canConnect && (
        <RemoteAgentDialog key={projectId} projectId={projectId} onClose={() => setConnect(false)} />
      )}
    </div>
  );
}
