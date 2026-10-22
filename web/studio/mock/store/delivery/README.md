# Mock delivery feedback

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Live delivery in Studio mock mode: publish runs, environment events, the Agent's delivery feedback phrases and the goal measurement of Live runs.

Importers: `mock/backend` and `mock/surface` import this directory.

Imports: it imports `lib/generated/protobuf`, `lib/server`, `lib/shared`, `mock/store`, `mock/store/scenarios` and `mock/store/setup`.

```mermaid
flowchart LR
  self["mock/store/delivery"]
  c0["mock/backend"] --> self
  c1["mock/surface"] --> self
  self --> d0["lib/generated/protobuf"]
  self --> d1["lib/server"]
  self --> d2["lib/shared"]
  self --> d3["mock/store"]
  self --> d4["mock/store/scenarios"]
  self --> d5["mock/store/setup"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

`delivery-scripts.ts` answers the delivery [script phrases](../../README.md#script-phrases)
(`fail live deployment`, `fail live build`) before the ordinary scripts of `mock/store`. A Live
deployment that fails in a phase the application owns is a `failure` notice that opens an
automatic turn, which reads the delivery status, pushes a fix and asks the user to publish it; an
unfinished build is a platform `notice` that only notifies. No Release, build or deployment runs;
the server's hooks are in `internal/dexai/delivery/releases` and `internal/dexai/delivery/deployments`.

`goal-measurement.ts` answers EvolveService for a Project with a Live deployment (NOT_FOUND before one, as the
server does): example goals of an invented outreach application (a reply rate with a target, a rate on few runs, a
guardrail that holds and an AI cost per qualified prospect in dollars) with three kept reviews (two measured, one
skipped because the Live version had no goals). Review now admits one review a day, refuses while one runs, and
measures it eight seconds later on the mock clock. The page query `goalsFixture=invalid` shows a goals file with
parser problems and `goalsFixture=none` one without goals; `goalsFixture=valid` brings the example back. Every number
is example data.
