// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useRef, useState } from "react";
import type { IScannerControls } from "@zxing/browser";
import { errorMessage } from "@/lib/api/transport/api-error";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { ERROR } from "@/components/flow/frame/workspace-styles";

/** Fills an Action field from a QR code read by the device camera; the field stays typeable. */
export function QrCodeScannerDialog({
  fieldDescription,
  onCancel,
  onScan,
}: {
  fieldDescription: string;
  onCancel: () => void;
  onScan: (value: string) => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const previewRef = useRef<HTMLVideoElement>(null);
  const controlsRef = useRef<IScannerControls | undefined>(undefined);
  const onScanRef = useRef(onScan);
  const [status, setStatus] = useState("Starting camera…");
  const [error, setError] = useState("");
  useEffect(() => {
    onScanRef.current = onScan;
  }, [onScan]);

  useEffect(() => {
    dialogRef.current?.querySelector<HTMLButtonElement>("[data-qr-close]")?.focus();
  }, []);

  useEffect(() => {
    const preview = previewRef.current;
    if (!preview) return undefined;
    let isCurrent = true;
    void startQRCodeScanner(preview, (value) => {
      if (isCurrent) onScanRef.current(value);
    })
      .then((controls) => {
        if (!isCurrent) {
          stopQRCodeScanner(controls, preview);
          return;
        }
        controlsRef.current = controls;
        setStatus("Point the camera at a QR code.");
      })
      .catch((scannerError: unknown) => {
        if (isCurrent) setError(qrScannerFailureMessage(scannerError));
      });
    return () => {
      isCurrent = false;
      stopQRCodeScanner(controlsRef.current, preview);
      controlsRef.current = undefined;
    };
  }, []);

  return (
    <Dialog aria-labelledby="qr-scanner-title" className="p-4.5" ref={dialogRef} onDismiss={onCancel}>
      <header className="flex items-start justify-between gap-3">
        <div>
          <h2 className="m-0 text-[17px]" id="qr-scanner-title">
            Scan QR code
          </h2>
          <p className="mt-1.5 text-xs text-ink-faint">{fieldDescription}</p>
        </div>
        <Button aria-label="Close QR scanner" data-qr-close variant="quiet" size="icon" onClick={onCancel}>
          ×
        </Button>
      </header>
      <video
        aria-label="QR camera preview"
        autoPlay
        className="mt-3.5 block aspect-[4/3] w-full rounded-lg bg-black object-cover"
        muted
        playsInline
        ref={previewRef}
      />
      {error ? (
        <p className={ERROR} role="alert">
          {error}
        </p>
      ) : (
        <p className="mt-1.5 text-xs text-ink-faint" role="status">
          {status}
        </p>
      )}
      <footer className="mt-3.5 flex justify-end">
        <Button size="sm" onClick={onCancel}>
          {error ? "Enter manually" : "Cancel"}
        </Button>
      </footer>
    </Dialog>
  );
}

/** Starts the rear camera and stops it after the first code it decodes. */
async function startQRCodeScanner(
  preview: HTMLVideoElement,
  onResult: (value: string) => void,
): Promise<IScannerControls> {
  if (!window.isSecureContext) {
    throw new Error("QR camera capture requires HTTPS or localhost.");
  }
  if (!navigator.mediaDevices?.getUserMedia) {
    throw new Error("This browser does not provide camera access.");
  }
  try {
    // The decoder is large and only needed once someone scans, so it loads on demand.
    const { BrowserQRCodeReader } = await import("@zxing/browser");
    return await new BrowserQRCodeReader().decodeFromConstraints(
      { audio: false, video: { facingMode: { ideal: "environment" } } },
      preview,
      (result, _error, controls) => {
        if (!result) return;
        stopQRCodeScanner(controls, preview);
        onResult(result.getText());
      },
    );
  } catch (error) {
    stopQRCodeScanner(undefined, preview);
    throw error;
  }
}

/** Releases the camera: the decoder's controls alone can leave the stream's tracks running. */
function stopQRCodeScanner(controls: IScannerControls | undefined, preview: HTMLVideoElement | null): void {
  try {
    controls?.stop();
  } catch (error) {
    console.error("Failed to stop QR scanner controls.", error);
  }
  if (!preview) return;
  const stream = preview.srcObject;
  if (!(stream instanceof MediaStream)) return;
  for (const track of stream.getTracks()) track.stop();
  preview.srcObject = null;
}

function qrScannerFailureMessage(error: unknown): string {
  const errorName = typeof error === "object" && error !== null && "name" in error ? String(error.name) : "";
  if (errorName === "NotAllowedError" || errorName === "SecurityError") {
    return "Camera access was denied. Allow camera access or enter the value manually.";
  }
  if (errorName === "NotFoundError" || errorName === "DevicesNotFoundError") {
    return "No camera was found. Enter the value manually.";
  }
  return errorMessage(error, "") || "The QR scanner could not start. Enter the value manually.";
}
