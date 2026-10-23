// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";

/** Compare the editable values, never the mere presence of an event. */
export function useCredentialDraft(editing: boolean, generation: number) {
  const root = useRef<HTMLDivElement>(null);
  const [dirty, setDirty] = useState(false);
  const acceptedKey = useRef("");
  const snapshot = () =>
    Array.from(root.current?.querySelectorAll("input") ?? []).map((input) =>
      input.type === "checkbox" ? input.checked : input.value,
    );
  const initial = useRef<(string | boolean)[]>([]);
  const clean = () => {
    acceptedKey.current = "";
    initial.current = snapshot();
    setDirty(false);
  };
  useEffect(() => {
    acceptedKey.current = "";
    initial.current = Array.from(root.current?.querySelectorAll("input") ?? []).map((input) =>
      input.type === "password" ? "" : input.type === "checkbox" ? input.checked : input.value,
    );
  }, [editing, generation]);
  const directInput = (event: FormEvent) => {
    const native = event.nativeEvent as InputEvent;
    return native.isTrusted && /^(insertText|insertFromPaste|insertFromDrop|delete.*)$/.test(native.inputType ?? "");
  };
  const update = (event?: FormEvent<HTMLDivElement>) => {
    const target = event?.target as HTMLInputElement | undefined;
    if (target?.type === "password" && event && !directInput(event)) target.value = "";
    // Controlled toggles mount/unmount endpoint fields when React commits this event.
    queueMicrotask(() => {
      if (root.current) setDirty(JSON.stringify(snapshot()) !== JSON.stringify(initial.current));
    });
  };
  const input = (event: FormEvent<HTMLDivElement>) => {
    const target = event.target as HTMLInputElement;
    if (target.type !== "password") return;
    // Password managers commonly emit a plain change/input or replacement event.
    // Only direct insertion, paste, drop and deletion may populate a write-only key.
    if (!directInput(event)) {
      target.value = "";
      update();
    } else acceptedKey.current = target.value;
  };
  const key = () => {
    const field = root.current?.querySelector<HTMLInputElement>('input[type="password"]');
    if (!field || field.value !== acceptedKey.current) {
      if (field) field.value = "";
      return "";
    }
    return acceptedKey.current;
  };
  return { root, dirty, setDirty, clean, update, input, key };
}
