// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef, useState, type ReactNode } from "react";
import { errorMessage } from "@/lib/api/transport/api-error";
import { Banner } from "@/components/ui/Banner";
import { Button, type ButtonVariant } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Dialog } from "@/components/ui/Dialog";
import { Eyebrow } from "@/components/ui/Eyebrow";

export interface DiagnosticOperation {
  submitting: boolean;
  error: string;
  submit: () => void;
}

/**
 * A Flow operation submitted from a dialog. Unmounting aborts the request in flight, and an
 * aborted request reports neither success nor failure.
 */
export function useDiagnosticOperation<T>(
  request: (signal: AbortSignal) => Promise<T>,
  onSucceeded: (result: T) => void,
  failure: string,
): DiagnosticOperation {
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const inFlight = useRef<AbortController | null>(null);
  useEffect(() => () => inFlight.current?.abort(), []);

  async function submit() {
    const controller = new AbortController();
    inFlight.current = controller;
    setSubmitting(true);
    setError("");
    try {
      const result = await request(controller.signal);
      if (!controller.signal.aborted) onSucceeded(result);
    } catch (requestError) {
      if (!controller.signal.aborted) setError(errorMessage(requestError, failure));
    } finally {
      if (!controller.signal.aborted) setSubmitting(false);
    }
  }

  return { submitting, error, submit: () => void submit() };
}

/** The label around one control of an operation dialog; the control comes as its child. */
export function OperationField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="mt-3.5 flex flex-col gap-1.5 text-[11px] font-bold text-ink-faint">
      {label}
      {children}
    </label>
  );
}

/** An operation dialog's input or textarea; its choices are Dropdowns. */
export const OPERATION_CONTROL = cx(
  "w-full rounded-lg border border-line-soft bg-bg px-2.5 py-2 text-[13px] font-normal text-ink outline-none focus:border-accent focus:shadow-(--focus-ring)",
);

/**
 * The modal around a Flow operation, the same in Runs and Diagnostics. It opens when mounted, so each
 * opening starts from fresh form state, and it cannot be dismissed while the operation is in flight.
 * Its submit button repeats the title; it reads as destructive unless the operation says otherwise.
 */
export function OperationDialog({
  titleId,
  eyebrow,
  title,
  closeLabel,
  dismissLabel,
  busyLabel,
  canSubmit,
  submitVariant = "danger",
  operation,
  onClose,
  children,
}: {
  titleId: string;
  /** A small line above the title; none when the title says enough. */
  eyebrow?: string;
  title: string;
  closeLabel: string;
  dismissLabel: string;
  busyLabel: string;
  canSubmit: boolean;
  submitVariant?: ButtonVariant;
  operation: DiagnosticOperation;
  onClose: () => void;
  children: ReactNode;
}) {
  const { submitting, error, submit } = operation;
  return (
    <Dialog
      aria-labelledby={titleId}
      className="p-5"
      onDismiss={() => {
        if (!submitting) onClose();
      }}
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          {eyebrow && <Eyebrow className="mb-1.5">{eyebrow}</Eyebrow>}
          <h2 id={titleId} className="m-0 text-base font-semibold tracking-normal text-ink">
            {title}
          </h2>
        </div>
        <Button aria-label={closeLabel} disabled={submitting} size="icon" onClick={onClose}>
          ×
        </Button>
      </div>
      {children}
      {error && <Banner className="mt-3.5">{error}</Banner>}
      <div className="mt-4.5 flex items-center justify-end gap-2">
        <Button disabled={submitting} onClick={onClose}>
          {dismissLabel}
        </Button>
        <Button variant={submitVariant} disabled={submitting || !canSubmit} onClick={submit}>
          {submitting ? busyLabel : title}
        </Button>
      </div>
    </Dialog>
  );
}
