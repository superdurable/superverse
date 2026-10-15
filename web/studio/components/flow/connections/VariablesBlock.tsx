// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useEffect, useState, useSyncExternalStore } from "react";
import { draftItemAnchor, draftItemKey, nextPublishItemAnchor } from "@/lib/api/flow/draft/configuration-draft";
import { environmentDraftItem, type EnvironmentValue } from "@/lib/api/flow/draft/scope-writes";
import { FOLLOW_PREVIEW, followsPreview, nextPublishEnvironmentItem } from "@/lib/api/flow/draft/next-publish-writes";
import {
  getApplicationEnvironment,
  type ApplicationEnvironmentField,
  type ApplicationEnvironmentSnapshot,
} from "@/lib/api/flow/application-environment";
import { useAnchorRequests, focusAnchorWithin } from "@/lib/browser/hash-focus";
import { Button } from "@/components/ui/Button";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { Dropdown } from "@/components/ui/menu/Dropdown";
import { DraftItemNote } from "@/components/flow/connector/native/DraftItemNote";
import { CAPTION, ONE_COLUMN_SETTING, TWO_COLUMN_SETTING } from "@/components/flow/connections/model/connection-words";
import { sideItem, SIDE_WORDS, type SetupSide } from "@/components/flow/connections/model/row-model";

const INPUT = cx(
  "h-7.5 w-full min-w-0 rounded-[7px] border bg-panel px-2.5 text-[12.5px] text-ink placeholder:text-ink-faint focus:border-accent-rule focus:shadow-(--focus-ring) focus:outline-none",
);
const CAPTIONS = { preview: "Preview", next: "Next publish" } as const;

/** An environment item's non-secret value, which the server encodes as `{"value": …}`; empty without one. */
function valueText(json: string): string {
  if (!json) return "";
  try {
    const value = (JSON.parse(json) as { value?: unknown }).value;
    return typeof value === "string" ? value : "";
  } catch {
    return "";
  }
}

/** A side's environment form, read again whenever the side's catalog does. */
function useEnvironment(side: SetupSide | null) {
  const [snapshot, setSnapshot] = useState<ApplicationEnvironmentSnapshot | null>(null);
  const client = side?.scope.client ?? null;
  const revision = side?.catalog.configurationRevision ?? "";
  useEffect(() => {
    if (!client) return;
    const controller = new AbortController();
    getApplicationEnvironment(client, controller.signal).then(
      (next) => !controller.signal.aborted && setSnapshot(next),
      // The rows stay as they were; the side's catalog says why it could not read.
      () => undefined,
    );
    return () => controller.abort();
  }, [client, revision]);
  return { snapshot, setSnapshot };
}

/**
 * One variable on one side: an input, a list for a closed set, or for a secret only whether it is set. The
 * next publish's starts as "Same as Preview" until "Different on Live…", and a ✕ goes back; a secret is
 * never copied from Preview, so Live enters its own.
 */
function VariableControl({
  side,
  field,
  snapshot,
  onWritten,
  live,
}: {
  side: SetupSide;
  field: ApplicationEnvironmentField;
  snapshot: ApplicationEnvironmentSnapshot;
  onWritten: (snapshot: ApplicationEnvironmentSnapshot) => void;
  live: string | null;
}) {
  const queue = side.queue;
  useSyncExternalStore(queue.subscribe, queue.getSnapshot, queue.getSnapshot);
  const [typed, setTyped] = useState<string | null>(null);
  const definition = side.catalog.definitionRevision;
  const key = draftItemKey({ kind: "KIND_ENVIRONMENT", environmentName: field.name });
  const { item, missing } = sideItem(side, key);
  const next = side.name === "next";
  const plain = environmentDraftItem(snapshot, definition, field.name, field.secret, onWritten);
  const own = next
    ? nextPublishEnvironmentItem(
        snapshot,
        definition,
        field.name,
        field.secret,
        onWritten,
        item?.state === "STATE_FOLLOWS_PREVIEW",
      )
    : null;
  const stored = queue.valueOf(key) ?? own?.base ?? plain.base;
  const following = next && !field.secret && followsPreview(stored);
  const value = followsPreview(stored) ? undefined : (stored as EnvironmentValue);
  const write = (update: EnvironmentValue | typeof FOLLOW_PREVIEW, immediate: boolean) =>
    own ? queue.save(own, () => update, immediate) : queue.save(plain, () => update as EnvironmentValue, immediate);
  const label = `${field.name} ${SIDE_WORDS[side.name]}`;
  const waiting =
    next && live && item?.differsFromLive && !field.secret
      ? `${live} runs ${valueText(item.liveValueJson) || "blank"} now. This goes out with the next publish.`
      : undefined;
  const previewText = valueText(item?.previewValueJson ?? "") || "blank";
  if (field.secret)
    return (
      <input
        aria-label={label}
        type="password"
        autoComplete="off"
        className={cx(INPUT, missing ? "border-fail-rule placeholder:text-fail" : "border-line-soft")}
        value={typed ?? ""}
        title={next ? "A secret is never copied from Preview" : undefined}
        placeholder={value?.configured ? "Set · type to replace" : next ? "Enter its own value" : "Not set"}
        onChange={(event) => {
          setTyped(event.target.value);
          if (event.target.value.length >= field.minLength)
            write({ configured: true, value: event.target.value }, false);
        }}
      />
    );
  const choices = field.enum.map((choice) => ({ value: choice, label: choice === "" ? "(empty string)" : choice }));
  if (following || field.enum.length > 0)
    return (
      <Dropdown
        label={label}
        tone={missing ? "none" : following ? "same" : "neutral"}
        pending={waiting}
        // A follower Setup misses shows what it resolves to, and its note says why.
        display={following && missing ? plain.base.value || "Not set" : undefined}
        note={
          following && missing
            ? `${plain.base.value ? `${plain.base.value} is Preview's value` : "Preview has no value yet"}, which the next publish follows; it needs one.`
            : undefined
        }
        align={next ? "end" : "start"}
        value={following ? "\u0000same" : (value?.value ?? null)}
        placeholder={field.enum.length ? "Choose a value…" : "blank"}
        onChange={(chosen) =>
          write(chosen === "\u0000same" ? FOLLOW_PREVIEW : { configured: true, value: chosen }, true)
        }
        items={[
          ...(next ? [{ value: "\u0000same", label: "Same as Preview", sub: previewText }] : []),
          ...(next && field.enum.length === 0
            ? [
                {
                  label: "Different on Live…",
                  action: () => {
                    // Live's own value starts as the one it follows now.
                    setTyped(plain.base.value ?? "");
                    write({ configured: true, value: plain.base.value ?? "" }, true);
                  },
                },
              ]
            : []),
          ...(next && choices.length ? ["separator" as const] : []),
          ...choices,
        ]}
      />
    );
  return (
    <span className="flex min-w-0 items-center gap-1">
      {waiting && (
        <span
          role="img"
          aria-label={waiting}
          title={waiting}
          className="mx-0.5 size-1.75 shrink-0 rounded-full bg-accent"
        />
      )}
      <input
        aria-label={label}
        className={cx(INPUT, missing ? "border-fail-rule" : "border-line-soft")}
        value={typed ?? value?.value ?? ""}
        placeholder="blank"
        onChange={(event) => {
          setTyped(event.target.value);
          if (event.target.value.length >= field.minLength)
            write({ configured: true, value: event.target.value }, false);
        }}
      />
      {next && (
        <Button
          variant="quiet"
          size="sm"
          title="Back to the same as Preview"
          aria-label={`${field.name}: back to the same as Preview`}
          onClick={() => {
            setTyped(null);
            write(FOLLOW_PREVIEW, true);
          }}
        >
          <Glyph name="close" className="size-3.5" />
        </Button>
      )}
    </span>
  );
}

/**
 * A variable's control on one side with what became of its autosave: a change made elsewhere since this tab
 * read it is never overwritten silently, and taking theirs shows it.
 */
function VariableCell(props: Parameters<typeof VariableControl>[0]) {
  const [generation, setGeneration] = useState(0);
  const key = draftItemKey({ kind: "KIND_ENVIRONMENT", environmentName: props.field.name });
  return (
    <>
      <VariableControl key={generation} {...props} />
      <DraftItemNote queue={props.side.queue} itemKey={key} onTheirs={() => setGeneration((value) => value + 1)} />
    </>
  );
}

/**
 * The values the code reads, under the connections: the required ones first, the optional ones folded.
 * Each has a control per side; a link to a variable unfolds them and focuses it.
 */
export function VariablesBlock({
  sides,
  liveTab,
  live,
}: {
  sides: (SetupSide | null)[];
  liveTab: boolean;
  live: string | null;
}) {
  const environments = [useEnvironment(sides[0] ?? null), useEnvironment(sides[1] ?? null)];
  const fields = environments[0].snapshot?.fields ?? environments[1].snapshot?.fields ?? [];
  const optional = fields.filter((field) => !field.required);
  const [unfolded, setUnfolded] = useState(false);
  const [container, setContainer] = useState<HTMLElement | null>(null);
  const [requested, setRequested] = useState<string | null>(null);
  const anchorOf = (field: ApplicationEnvironmentField) =>
    draftItemKey({ kind: "KIND_ENVIRONMENT", environmentName: field.name });
  const anchors = new Set(
    fields.flatMap((field) => [draftItemAnchor(anchorOf(field)), nextPublishItemAnchor(anchorOf(field))]),
  );
  useAnchorRequests(
    (id) => anchors.has(id),
    (id) => {
      setUnfolded(true);
      setRequested(id);
    },
  );
  useEffect(() => {
    if (!requested || !container) return;
    const frame = requestAnimationFrame(() => {
      focusAnchorWithin(container, requested);
      setRequested(null);
    });
    return () => cancelAnimationFrame(frame);
  }, [requested, container, unfolded]);
  if (fields.length === 0) return null;
  const row = (field: ApplicationEnvironmentField) => (
    <div
      key={field.name}
      id={draftItemAnchor(anchorOf(field))}
      className={cx(liveTab ? ONE_COLUMN_SETTING : TWO_COLUMN_SETTING, "min-h-11")}
      data-testid="setup-variable"
    >
      <span className={cx("min-w-0", liveTab ? "@max-[520px]/tool:col-span-full" : "@max-[780px]/tool:col-span-full")}>
        <span className="font-mono text-[12.5px] text-ink">{field.name}</span>
        <span className="block text-[11.5px] text-ink-faint">
          {[field.required ? "required" : "optional", field.secret ? "secret" : ""].filter(Boolean).join(" · ")}
        </span>
      </span>
      {sides.map((side, index) => {
        const snapshot = environments[index].snapshot;
        const own = snapshot?.fields.find((entry) => entry.name === field.name);
        return (
          <span
            key={side?.name ?? index}
            id={side?.name === "next" ? nextPublishItemAnchor(anchorOf(field)) : undefined}
            className="flex min-w-0 flex-col gap-0.75"
          >
            {!liveTab && side && <small className={CAPTION}>{CAPTIONS[side.name]}</small>}
            {side && snapshot && own ? (
              <VariableCell
                side={side}
                field={own}
                snapshot={snapshot}
                onWritten={environments[index].setSnapshot}
                live={live}
              />
            ) : (
              <span className="text-[12px] text-ink-faint">Reading…</span>
            )}
          </span>
        );
      })}
      {!liveTab && <span className="@max-[780px]/tool:hidden" />}
    </div>
  );
  return (
    <section aria-label="Variables" ref={setContainer} data-testid="setup-variables">
      <h3 className="mt-5.5 mb-2 flex items-center gap-2 text-[11px] font-normal tracking-[0.07em] text-ink-faint uppercase">
        Variables
      </h3>
      <div className="rounded-[10px] border border-line-soft bg-panel py-1">
        {fields.filter((field) => field.required).map(row)}
        {optional.length > 0 && (
          <div className="flex min-h-10 items-center px-3.5">
            <button
              type="button"
              aria-expanded={unfolded}
              onClick={() => setUnfolded(!unfolded)}
              className="inline-flex items-center gap-1.5 text-[12.5px] text-ink-dim hover:text-ink"
            >
              <Glyph name="chevronDown" className={cx("size-3.5 transition-transform", unfolded && "rotate-180")} />
              {optional.length} optional
            </button>
          </div>
        )}
        {unfolded && optional.map(row)}
      </div>
    </section>
  );
}
