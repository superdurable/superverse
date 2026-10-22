# Browser state

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Browser-only utilities: storage with every stored key, the theme preference, single-flight user actions, URL query updates and paged lists.

Importers: `app`, `components/build`, `components/flow/**` (5), `components/frame/**` (3), `components/project/**` (3), `components/publishing/**` (3), `components/session/**` (3), `components/shell`, `components/teamspace/**` (2), `components/ui/**` (2), `lib/project/**` (3), `lib/session/**` (2) and `lib/teamspace` import this directory.

Imports: it imports `lib/api/transport` and `lib/shared`.

```mermaid
flowchart LR
  self["lib/browser"]
  c0["app"] --> self
  c1["components/build"] --> self
  c2["components/flow/** (5)"] --> self
  c3["components/frame/** (3)"] --> self
  c4["components/project/** (3)"] --> self
  c5["components/publishing/** (3)"] --> self
  c6["components/session/** (3)"] --> self
  c7["components/shell"] --> self
  c8["components/teamspace/** (2)"] --> self
  c9["components/ui/** (2)"] --> self
  c10["lib/project/** (3)"] --> self
  c11["lib/session/** (2)"] --> self
  c12["lib/teamspace"] --> self
  self --> d0["lib/api/transport"]
  self --> d1["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

`useQueryUpdate` builds link hrefs from the rendered query, but applies replacements to the browser URL at call
time. `StudioLink` owns one pending client destination: the Builder's links and programmatic tool pushes
record it before navigation starts. Query replacements merge into that destination, so automatic Flow and Live
release selections preserve a click still awaiting its server response. An effect from a page being left cannot
edit the destination's query. A merged pending push keeps the click's history entry; a standalone replacement
adds none. A committed URL change clears the record; another navigation replaces it. No
per-navigation history, timer, server state or external call is retained.
