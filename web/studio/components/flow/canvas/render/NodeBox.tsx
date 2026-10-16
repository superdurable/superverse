// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { JSX, KeyboardEvent } from "react";
import type { Box, BoxRow } from "@/components/flow/canvas/layout/scene";
import { cx } from "@/components/ui/cx";
import { Glyph } from "@/components/ui/Glyph";
import { cardStyle, nowRing, REASON_TONE, ROW_TONE, rpcCardStyle, SEGMENT } from "./box-styles";
import { ChipRows, RunPillView, StartPill, TileView, TONE_TEXT } from "./CardParts";

/**
 * One Step or RPC card as plain DOM: a `div[role=button]` rather than a `<button>`, because the jump
 * chips and the outcome bar inside are themselves buttons and interactive controls must not nest.
 * Its measurements are layout/step-card.ts's; change them together.
 */

function Row({ row }: { row: BoxRow }): JSX.Element {
  return (
    <span
      className={cx(
        "flex items-baseline gap-1.25 text-[13px]/normal whitespace-nowrap",
        ROW_TONE[row.tone ?? "normal"],
      )}
      data-tone={row.tone}
    >
      {row.glyph ? <span className="w-[11px] flex-none text-xs text-ink-faint">{row.glyph}</span> : null}
      <span className="overflow-hidden text-ellipsis">{row.text}</span>
      {row.trail ? (
        <span className="ml-auto flex-none pl-1.5 font-mono text-xs text-ink-faint">{row.trail}</span>
      ) : null}
    </span>
  );
}

export function keyActivates(e: KeyboardEvent<HTMLElement>, act: () => void): void {
  if (e.key !== "Enter" && e.key !== " ") return;
  e.preventDefault();
  act();
}

/** An RPC of a Flow without Steps, listing the state it reads and writes. */
function RpcCard({ box, selected, onSelect }: { box: Box; selected: boolean; onSelect: (id: string) => void }) {
  return (
    <div
      role="button"
      tabIndex={0}
      className={cx("nodrag nopan", rpcCardStyle(box, selected))}
      data-kind={box.kind}
      data-pick={box.title}
      data-rpc-role={box.rpcRole}
      data-selected={selected ? "true" : undefined}
      style={{ width: `${box.w}px`, height: `${box.h}px` }}
      onClick={() => onSelect(box.id)}
      onKeyDown={(e) => keyActivates(e, () => onSelect(box.id))}
    >
      <span className="truncate text-[15px] font-semibold tracking-[-0.005em] before:text-door before:content-['✉_']">
        {box.title}
      </span>
      {box.subtitle ? <span className="truncate text-[12.5px] text-ink-faint">{box.subtitle}</span> : null}
      {box.rows && box.rows.length > 0 ? (
        <span className="flex flex-col gap-px">
          {box.rows.map((r, i) => (
            <Row key={`${r.text}-${i}`} row={r} />
          ))}
        </span>
      ) : null}
    </div>
  );
}

/** The failure count beside a card's title: how many failures leave it, drawn when it is selected. */
function FailureCount({ count }: { count: number }): JSX.Element {
  const text = `${count} failure route${count === 1 ? "" : "s"} leave${count === 1 ? "s" : ""} here. Select to see where ${count === 1 ? "it goes" : "they go"}.`;
  return (
    <span
      className="flex-none self-start rounded-full border border-fail-rule/60 px-1.25 text-[10.5px]/[15px] font-semibold text-fail-rule"
      title={text}
      data-failures={count}
    >
      ⚠ {count}
    </span>
  );
}

/** In a failure detour: a failure that goes straight to a main-story Step drawn elsewhere. */
function Pointer({ box, onJump }: { box: Box; onJump: (id: string) => void }): JSX.Element {
  const go = (): void => {
    if (box.target) onJump(box.target);
  };
  return (
    <button
      type="button"
      className="nodrag nopan flex size-full cursor-pointer items-center truncate rounded-full border border-dashed border-fail-rule bg-fail-subtle px-3 text-[12px] font-medium text-fail hover:outline hover:outline-mark focus-visible:outline-2 focus-visible:outline-accent"
      data-kind="pointer"
      title={`On failure: goes to ${box.title.replace(/^↪ /, "")}. Click to show it.`}
      style={{ width: `${box.w}px`, height: `${box.h}px` }}
      onClick={go}
    >
      {box.title}
    </button>
  );
}

export function NodeBox({
  box,
  selected,
  now = false,
  onSelect,
  onJump,
}: {
  box: Box;
  selected: boolean;
  /** The Step the shown run is at now: ringed and labelled. */
  now?: boolean;
  onSelect: (id: string) => void;
  /** A jump chip was clicked: show where it leads. */
  onJump: (id: string) => void;
}): JSX.Element {
  if (box.kind === "rpcCard") return <RpcCard box={box} selected={selected} onSelect={onSelect} />;
  if (box.kind === "pointer") return <Pointer box={box} onJump={onJump} />;
  const select = (): void => onSelect(box.id);
  return (
    <div
      role="button"
      tabIndex={0}
      className={cx("nodrag nopan", cardStyle(box, selected), now && nowRing(box))}
      data-kind={box.kind}
      data-pick={box.title}
      data-step-type={box.identity}
      data-now={now ? "true" : undefined}
      data-actor={box.actor}
      data-start={box.isStart ? "true" : undefined}
      data-emphasis={box.emphasis}
      data-selected={selected ? "true" : undefined}
      data-detour={box.detourOf !== undefined ? "true" : undefined}
      aria-label={`${box.title}${box.subtitle ? `, ${box.subtitle}` : ""}${box.pill ? `, ${box.pill.text}` : ""}${now ? ", now" : ""}${box.failures ? `, ${box.failures} failure route${box.failures === 1 ? "" : "s"}` : ""}${box.detourOf !== undefined ? ", failure handling" : ""}${box.unwritten ? ", not written yet" : ""}${box.mark ? `, ${box.mark}` : ""}`}
      title={box.identity}
      style={{ width: `${box.w}px`, height: `${box.h}px` }}
      onClick={select}
      onKeyDown={(e) => keyActivates(e, select)}
    >
      {box.isStart ? <StartPill /> : null}
      {box.pill ? <RunPillView pill={box.pill} /> : null}
      <span className="flex min-w-0 items-start gap-2.5">
        {box.tile ? <TileView tile={box.tile} /> : null}
        <span className="flex min-w-0 flex-1 flex-col">
          <span
            className={cx(
              "line-clamp-2 text-[13.5px]/[18px] font-semibold tracking-[-0.005em] break-words",
              box.emphasis === "planned" && "text-ink-faint",
            )}
          >
            {box.title}
          </span>
          {box.subtitle ? (
            <span
              className={cx(
                "truncate text-[11.5px]/4",
                box.subtitleTone ? TONE_TEXT[box.subtitleTone] : "text-ink-faint",
              )}
            >
              {box.subtitle}
            </span>
          ) : null}
        </span>
        {/* BPMN's loop marker: the Step can run again on its own. */}
        {box.loops ? (
          <span title="This step can run again on its own" className="mt-0.5 flex-none text-ink-faint">
            <Glyph name="refresh" className="size-3.5" />
          </span>
        ) : null}
        {box.failures ? <FailureCount count={box.failures} /> : null}
        {/* ⇥ a sink, ⇉ a dispatcher: a saga catching its own failures is structure, not a warning. */}
        {box.recovery ? (
          <span className="flex-none text-[14px] leading-none text-fail-rule" title={box.recovery.title}>
            {box.recovery.glyph}
          </span>
        ) : null}
      </span>

      <ChipRows rows={box.chips ?? []} onJump={onJump} />

      {/* Each segment's width is its count, so one failure among many stays visible. */}
      {box.bar && box.bar.length > 0 ? (
        <button
          type="button"
          className="mt-1.5 flex h-[5px] gap-px overflow-hidden rounded-[3px] hover:outline hover:outline-offset-1 hover:outline-mark focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ink-dim"
          title={box.barTitle}
          aria-label={box.barTitle ?? "execution outcomes"}
          onClick={(e) => {
            e.stopPropagation();
            select();
          }}
        >
          {box.bar.map((seg) => (
            <span
              className={cx("min-w-[2px]", SEGMENT[seg.status])}
              data-status={seg.status}
              key={seg.status}
              style={{ flexGrow: seg.count }}
            />
          ))}
        </button>
      ) : null}

      {box.reason ? (
        <span
          className={cx(
            "mt-2 truncate border-t border-line-faint pt-1 text-[12px]/[15px]",
            REASON_TONE[box.reason.tone],
          )}
          data-tone={box.reason.tone}
          title={box.reason.text}
        >
          {box.reason.text}
        </span>
      ) : null}
    </div>
  );
}
