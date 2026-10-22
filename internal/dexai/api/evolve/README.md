# Goal measurement business API

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Serves EvolveService: a Project's goal measurement, with its goals, schedule and reviews, and a person's Review now.

Callers: `internal/dexai/api` imports this package.

Dependencies: it imports `gen/superverse/v2`, `internal/dexai/api/boundary`, `internal/dexai/delivery/evolve`, `internal/dexai/delivery/evolve/goals` and `internal/dexai/projects`.

```mermaid
flowchart LR
  self["internal/dexai/api/evolve"]
  c0["internal/dexai/api"] --> self
  self --> d0["gen/superverse/v2"]
  self --> d1["internal/dexai/api/boundary"]
  self --> d2["internal/dexai/delivery/evolve"]
  self --> d3["internal/dexai/delivery/evolve/goals"]
  self --> d4["internal/dexai/projects"]
```
<!-- END GENERATED PACKAGE SUMMARY -->

`EvolveService` fronts the Project's `EvolveFlow` ([goal measurement](../../delivery/evolve/README.md)).
The read model authorizes every call before the Flow is reached. Live published runs only (owner
rule 2026-10-22): every goal value and review it answers was measured from runs of the Project's
Live, published deployment against the goals file of the Live build's commit; Preview, workspace
test runs and any other engine never feed them, so there is no Preview measurement to answer.

| RPC | Authorization | Does |
|---|---|---|
| `GetEvolve` | `CapabilityOperateLive` | The Flow's `GetEvolve`: the Live release it was told of, the goals of the Live build (parsed goals or the parser's errors), the schedule, the kept reviews newest first with their goal values, whether agent reviews follow measurement for the Project (`evolve.reviewAccess` and `evolve.reviewTeamSpaces`) and when Review now is admitted again. `NOT_FOUND` before the Project's first Live deployment, which Studio shows as the empty state |
| `StartEvolveReview` | `CapabilityBuild` | The Flow's `StartReview` with Studio's `request_id` and the caller as actor. A refusal (`NO_GOALS`, `GOALS_INVALID`, `REVIEW_RUNNING`, `RATE_LIMITED`) is an answer, not an error; a repeated `request_id` answers the review it started. `NOT_FOUND` before the first Live deployment |

Closed sets map to their protobuf enums with `boundary.Enum`; times are absent when unset.
