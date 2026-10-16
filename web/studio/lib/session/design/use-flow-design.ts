// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState } from "react";
import { getFlowDesign } from "@/lib/api/agent";
import { ApiError, errorMessage, isRevisionConflict } from "@/lib/api/transport/api-error";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import type { DesignPhase } from "@/lib/shared/design-phase";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { preparedSourceOf, readyPreparedSource } from "@/lib/session/session-status";
import { useSessionResource } from "@/lib/session/SessionProvider";

/** The body of a Flow design, as GetFlowDesign returns it. */
type FlowDesignBody = NonNullable<RpcOutput<"GetFlowDesign">["design"]>;

export interface ShownDesign {
  /** The card version and the design's digest, which the Flow tab shows by itself at most once. */
  key: string;
  phase: DesignPhase;
  summary: string;
  outline: string;
  /** The features of its requirements, which the Steps name. */
  features: readonly { id: string; text: string }[];
  /** The designed Steps whose declaration the agent wrote since the design was recorded, each `Flow.Step`. */
  writtenSteps: readonly string[];
  /** Null while it loads and after a failed read. */
  body: FlowDesignBody | null;
  error: string | null;
  retry: () => void;
}

type Slot = Required<RpcInput<"GetFlowDesign">>["slot"];

interface DesignReference {
  slot: Slot;
  digest: string;
  summary: string;
  outline: string;
  phase: DesignPhase;
  version: number;
  features: readonly { id: string; text: string }[];
  writtenSteps: readonly string[];
}

/**
 * The design the Session's Snapshot says the Flow tab draws: the current version's, from the moment the
 * agent records its card until it is built and the Preview's processes are built from its push. A later
 * push moves past it.
 */
function shownReference(resource: SessionSnapshot__Output | null | undefined): DesignReference | null {
  const description = resource?.agent?.description;
  if (!resource || !description) return null;
  const confirmed = description.confirmedRequirements;
  const revision = Number(confirmed?.revision ?? 0);
  const spec = confirmed?.spec;
  if (!spec?.design) return null;
  if (spec.designBuilt) {
    const builtHead = spec.designBuiltCommit === resource.headCommit;
    if (!builtHead || readyPreparedSource(preparedSourceOf(resource), resource.headCommit)) return null;
  }
  return {
    slot: "AGENT_FLOW_DESIGN_SLOT_CONFIRMED",
    ...spec.design,
    phase: spec.designBuilt ? "built" : "building",
    version: revision,
    features: spec.features,
    writtenSteps: spec.designWrittenSteps,
  };
}

/** Bodies by digest, so a Watch frame with the same reference reads nothing; shared by every reader. */
const bodies = new Map<string, FlowDesignBody>();
/** The read of each digest in flight, which every reader of that digest awaits. */
const reads = new Map<string, Promise<FlowDesignBody | null>>();

function readBody(projectId: string, slot: Slot, digest: string): Promise<FlowDesignBody | null> {
  let read = reads.get(digest);
  if (!read) {
    read = getFlowDesign({ projectId, slot, digest }, new AbortController().signal)
      .then(({ design }) => {
        if (design) bodies.set(digest, design);
        return design;
      })
      .finally(() => reads.delete(digest));
    reads.set(digest, read);
  }
  return read;
}

/**
 * The Flow design the Flow tab draws, with its body read once per digest; null when it draws none or the
 * reader is not `enabled`. A stale read re-reads the Snapshot, whose new reference reads again; a slot
 * without a design draws none.
 */
export function useFlowDesign(enabled: boolean): ShownDesign | null {
  const session = useSessionResource();
  const reference = enabled ? shownReference(session?.resource) : null;
  const projectId = session?.projectId ?? "";
  const refresh = session?.refresh;
  const slot = reference?.slot;
  const digest = reference?.digest ?? "";
  const [read, setRead] = useState<{ digest: string; error: string | null; absent: boolean } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!slot || !digest || bodies.has(digest)) return undefined;
    // The read is shared, so a closed reader only stops listening.
    const controller = new AbortController();
    readBody(projectId, slot, digest).then(
      (design) => {
        if (!controller.signal.aborted) setRead({ digest, error: null, absent: !design });
      },
      (reason: unknown) => {
        if (controller.signal.aborted) return;
        if (isRevisionConflict(reason)) {
          void refresh?.();
          return;
        }
        const absent = reason instanceof ApiError && reason.status === 404;
        setRead({ digest, error: absent ? null : errorMessage(reason, "The Flow design did not load"), absent });
      },
    );
    return () => controller.abort();
  }, [attempt, digest, projectId, refresh, slot]);
  if (!reference) return null;
  const current = read?.digest === digest ? read : null;
  if (current?.absent) return null;
  return {
    key: `${reference.version}:${digest}`,
    phase: reference.phase,
    summary: reference.summary,
    outline: reference.outline,
    features: reference.features,
    writtenSteps: reference.writtenSteps,
    body: bodies.get(digest) ?? null,
    error: current?.error ?? null,
    retry: () => {
      setRead(null);
      setAttempt((value) => value + 1);
    },
  };
}
