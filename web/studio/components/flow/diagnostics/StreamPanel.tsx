// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { hydrateBlobs } from "@/lib/api/flow/flow-blobs";
import { listenToStream } from "@/lib/api/flow/flow-diagnostics";
import { formatDate } from "@/lib/shared/format";
import { errorMessage } from "@/lib/api/transport/api-error";
import type { StreamMessage } from "@/lib/api/flow/flow-types";
import { useFlowRequestScope } from "@/lib/flow/FlowApiProvider";
import { StructuredValue } from "@/components/flow/diagnostics/value/StructuredValue";
import { Banner } from "@/components/ui/Banner";
import { Button } from "@/components/ui/Button";
import { Eyebrow } from "@/components/ui/Eyebrow";
import { cx } from "@/components/ui/cx";
import { OPERATION_CONTROL } from "./OperationDialog";

interface StreamPanelProps {
  flowId: string;
  flowType: string;
}

export function StreamPanel({ flowId, flowType }: StreamPanelProps) {
  const api = useFlowRequestScope(`${flowId}:${flowType}`);
  const [streamName, setStreamName] = useState("");
  const [activeStreamName, setActiveStreamName] = useState("");
  const [messages, setMessages] = useState<StreamMessage[]>([]);
  const [isListening, setIsListening] = useState(false);
  const [error, setError] = useState("");
  const [blobWarning, setBlobWarning] = useState("");
  const [session, setSession] = useState(0);
  const blobCache = useRef(new Map<string, unknown>());

  useEffect(() => {
    if (!activeStreamName) return;
    const controller = new AbortController();

    const read = async () => {
      setIsListening(true);
      try {
        await listenToStream(
          api,
          { flowId, flowType, streamName: activeStreamName },
          controller.signal,
          async (rawMessage) => {
            const hydrated = await hydrateBlobs(flowId, rawMessage.value, blobCache.current, api, controller.signal);
            if (controller.signal.aborted) return;
            // The panel keeps the newest 200 messages and bounds the blob cache the same way.
            setMessages((current) => [...current.slice(-199), { ...rawMessage, value: hydrated.value }]);
            if (blobCache.current.size > 1000) blobCache.current.clear();
            if (hydrated.error) setBlobWarning(hydrated.error);
          },
        );
      } catch (readError) {
        if (controller.signal.aborted) return;
        setError(errorMessage(readError, "Stream read failed"));
        setIsListening(false);
      }
    };
    void read();
    return () => controller.abort();
  }, [api, activeStreamName, flowId, flowType, session]);

  const startListening = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const nextStreamName = streamName.trim();
    if (!nextStreamName) {
      setError("Enter a Stream name.");
      return;
    }
    blobCache.current.clear();
    setMessages([]);
    setError("");
    setBlobWarning("");
    setActiveStreamName(nextStreamName);
    setSession((current) => current + 1);
  };

  return (
    <section className="grid max-w-240 gap-4.5 pt-1 pb-7">
      <div className="flex items-start justify-between gap-5 max-md:flex-col max-md:items-stretch">
        <div>
          <Eyebrow className="mb-1.5">Resumable stream</Eyebrow>
          <h2 className="mt-0.75 mb-1.5 text-sm font-semibold tracking-normal text-ink">Stream messages</h2>
          <p className="max-w-165 text-[13px]/[1.55]">
            Messages are best effort and may be trimmed as the Stream reaches its configured capacity. This view retains
            the most recent 200 messages.
          </p>
        </div>
        {activeStreamName && (
          <span
            className="inline-flex flex-none items-center gap-1.75 rounded-full border border-line-soft bg-panel px-2.5 py-1.75 text-xs font-bold text-ink-faint max-md:self-start"
            aria-live="polite"
          >
            <span
              className={cx(isListening && "size-1.75 rounded-full bg-cta shadow-[0_0_0_3px_var(--color-pass-subtle)]")}
            />
            {isListening ? "Listening" : "Stopped"}
          </span>
        )}
      </div>

      <form className="grid max-w-140 gap-1.75" onSubmit={startListening}>
        <label className="text-xs font-bold text-ink-faint" htmlFor="stream-name">
          Stream name
        </label>
        <div className="flex gap-2 max-md:flex-col max-md:items-stretch">
          <input
            className={cx(OPERATION_CONTROL, "min-w-0 flex-1")}
            id="stream-name"
            name="streamName"
            onChange={(event) => setStreamName(event.target.value)}
            placeholder="a defined stream name"
            value={streamName}
          />
          <Button variant="accent" type="submit">
            {activeStreamName ? "Restart listening" : "Start listening"}
          </Button>
        </div>
      </form>

      {error && <Banner>{error}</Banner>}
      {blobWarning && <Banner tone="busy">{blobWarning}</Banner>}

      {activeStreamName && messages.length === 0 && !error && (
        <p className="mt-1 rounded-xl border border-dashed border-line-soft p-5.5 text-center text-ink-faint">
          Listening for messages in <code>{activeStreamName}</code>…
        </p>
      )}
      {messages.length > 0 && (
        <div className="grid max-h-[min(60vh,720px)] gap-2.5 overflow-y-auto pr-1" aria-live="polite">
          {messages.map((message) => (
            <article
              className="rounded-xl border border-line-soft bg-panel p-3.5 shadow-[0_3px_12px_rgba(45,79,42,0.04)]"
              key={message.resumeToken}
            >
              <div className="mb-2.5 flex items-center gap-2.5 text-xs text-ink-faint">
                <span>{formatDate(message.createdTime)}</span>
                {message.source && (
                  <code className="max-w-[70%] truncate rounded-sm bg-raised px-1.25 py-0.5">{message.source}</code>
                )}
              </div>
              <StructuredValue value={message.value} />
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
