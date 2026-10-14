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
import { SessionConnectionIndicator, WorkspaceStatusIndicator } from "./SessionStatusIndicators";

/** Beside the chat's heading: whether live updates are connected, and how long the workspace is kept. */
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
    <div className="flex items-center gap-2">
      <SessionConnectionIndicator />
      <WorkspaceStatusIndicator />
      <SessionExecutorStatus canManage={canConnect} onConnect={() => setConnect(true)} />
      {connect && canConnect && (
        <RemoteAgentDialog key={projectId} projectId={projectId} onClose={() => setConnect(false)} />
      )}
    </div>
  );
}
