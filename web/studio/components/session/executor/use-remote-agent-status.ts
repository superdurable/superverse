// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState } from "react";
import { useSessionResource } from "@/lib/session/SessionProvider";
import { timestampToISO } from "@/lib/shared/protobuf";

/** SQL last-seen information is presentation only; the canonical executor owns authority. */
export function useRemoteAgentStatus() {
  const session = useSessionResource();
  const executor = session?.resource?.agent?.description?.executor;
  const remote = executor?.mode === "SESSION_EXECUTOR_MODE_REMOTE" ? executor.remote : null;
  const paired = remote ?? executor?.suspended;
  const seen = timestampToISO(paired?.lastSeenAt);
  const connection = paired?.connectionId;
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!connection) return;
    const timer = window.setInterval(() => setNow(Date.now()), 15_000);
    return () => window.clearInterval(timer);
  }, [connection]);
  const online = seen !== null && Number.isFinite(Date.parse(seen)) && now - Date.parse(seen) <= 90_000;
  return { session, executor, remote, seen, online };
}
