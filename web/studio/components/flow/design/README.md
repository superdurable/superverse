# Flow design view

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Draws an agent's Flow design with the Flow canvas under its Design label, and compares it with the current source for its marks and the card's change lines.

Importers: `components/flow/scope` imports this directory.

Imports: it imports `components/flow/canvas`, `components/flow/canvas/model`, `components/flow/frame`, `components/ui`, `lib/api/flow`, `lib/generated/protobuf` and `lib/shared`.

```mermaid
flowchart LR
  self["components/flow/design"]
  c0["components/flow/scope"] --> self
  self --> d0["components/flow/canvas"]
  self --> d1["components/flow/canvas/model"]
  self --> d2["components/flow/frame"]
  self --> d3["components/ui"]
  self --> d4["lib/api/flow"]
  self --> d5["lib/generated/protobuf"]
  self --> d6["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

A design is the optional `design` of a requirements card, read through `GetFlowDesign`
([design before implementation](../../../../../docs/architecture/design-first/studio.md)). Its hosts in
`components/build` and `components/session` reach it only through `components/flow/scope`.

| File                  | Responsibility                                                                                                                                                                                                               |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `design-graph.ts`     | One designed Flow as the subset of Flow Definition Graph 2.0 the canvas decoder reads: Steps, groups, waits, decisions, failure routes, Channels, Actions and Attributes, with `source.language` "design" and `valid: false` |
| `design-compare.ts`   | A design against the current source per Flow type: new, changed or unchanged Flows, the marks of the designed Steps and the card's change lines                                                                              |
| `DesignFlowView.tsx`  | The Flow tab's design view: the label, the purpose, what it removes and the marked canvas, through `FlowGraphCanvas`                                                                                                         |
| `DesignStepPanel.tsx` | A designed Step's explanation, wait, ways out and the features it serves                                                                                                                                                     |

The expansion goes through the canvas's own decoder and its split of the main story from failure
handling, so a design and its implementation draw the same picture. A designed Flow never enters a
catalog, so it has no Runs, Start or Action control.
