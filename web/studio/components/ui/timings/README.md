# components/ui/timings

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: The list of the phases a Preview start, a build or a deployment measured, each with its label and duration.

Importers: `components/build`, `components/project/activity` and `components/session` import this directory.

Imports: it imports `lib/generated/protobuf` and `lib/shared`.

```mermaid
flowchart LR
  self["components/ui/timings"]
  c0["components/build"] --> self
  c1["components/project/activity"] --> self
  c2["components/session"] --> self
  self --> d0["lib/generated/protobuf"]
  self --> d1["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->
