// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { getProjectConfiguration, type ConfigurationTarget } from "@/lib/api/project";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";

/**
 * Reads one configuration target, and reads it again whenever `version`
 * changes or `retry` is called. A re-read keeps the configuration on screen
 * until the new one arrives, and never replaces a configuration that an action
 * set after the re-read began; a null target reads nothing.
 */
export function useProjectConfiguration(target: ConfigurationTarget | null, version = "") {
  const targetKey = target && JSON.stringify(target);
  const [readVersion, setReadVersion] = useState(0);
  const requestKey = JSON.stringify([targetKey, version, readVersion]);
  const [state, setState] = useState<{
    targetKey: string;
    requestKey: string;
    configuration: ProjectConfigurationSnapshot__Output | null;
    error: string;
  } | null>(null);
  // Counts the configurations that actions set, so a read knows whether one
  // arrived while it was in flight.
  const writes = useRef(0);

  useEffect(() => {
    if (!targetKey) return;
    const controller = new AbortController();
    const writesBefore = writes.current;
    getProjectConfiguration({ target: JSON.parse(targetKey) as ConfigurationTarget }, controller.signal).then(
      (configuration) => {
        if (controller.signal.aborted) return;
        setState((current) => ({
          targetKey,
          requestKey,
          configuration:
            writes.current !== writesBefore && current?.targetKey === targetKey ? current.configuration : configuration,
          error: "",
        }));
      },
      (reason: unknown) => {
        if (!controller.signal.aborted)
          setState((current) => ({
            targetKey,
            requestKey,
            configuration: current?.targetKey === targetKey ? current.configuration : null,
            error: errorMessage(reason, "Configuration unavailable"),
          }));
      },
    );
    return () => controller.abort();
  }, [targetKey, requestKey]);

  const retry = useCallback(() => setReadVersion((current) => current + 1), []);

  // An action's configuration is newer than any read in flight, which keeps
  // loading until it answers; a result for another target is dropped.
  const setConfiguration = useCallback(
    (configuration: ProjectConfigurationSnapshot__Output) => {
      if (!targetKey) return;
      writes.current += 1;
      setState((current) =>
        current && current.targetKey !== targetKey
          ? current
          : { targetKey, requestKey: current?.requestKey ?? requestKey, configuration, error: "" },
      );
    },
    [targetKey, requestKey],
  );
  const current = state?.targetKey === targetKey ? state : null;
  return {
    configuration: current?.configuration ?? null,
    error: current?.requestKey === requestKey ? current.error : "",
    loaded: current !== null && (current.configuration !== null || current.requestKey === requestKey),
    loading: targetKey !== null && current?.requestKey !== requestKey,
    retry,
    setConfiguration,
  };
}
