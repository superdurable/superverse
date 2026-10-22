# Goals and their measurement

Use this reference when the person asks to set, change or fix the
application's goals, when Studio's Goals tab hands you "Help me set goals for
this app" or the problems of a goals file, or when someone asks how the
application is doing against its goals. It covers the goals file, the metric
language and how the platform measures them.

## Only Live published runs count

The platform's rule (set 2026-10-22), word for word:

> Only runs of the Live, published process count. Goals apply only to
> published runs. Preview runs (the Sandbox-local engine at 127.0.0.1:8801,
> decision 0013), workspace test runs (apptest) and any other engine never
> feed goal values, metrics, reviews, backtests, the open-run gate, trials,
> proposals or anything Studio shows as a goal value. Goals are the goals file
> at the Live build's commit, never the workspace draft. Runs started on an
> earlier Live release still count: they were published.

"Decision 0013" is the platform's own design of the Preview engine. You have
no copy of it and need none, so do not look for it.

What it means for your work:

- Preview runs and test runs are tests, never evidence. Never compute,
  estimate or report a goal value, a baseline, a metric or a backtest from
  `dexcli` reads of the Preview engine, from `make test-app` runs or from any
  other engine, and never suggest running the Preview or the tests "to see the
  goals work".
- Goal values come only from the platform's reviews of the Live runs, which
  the Goals tab shows on Live. To look at single Live runs, use
  `read_live_app_runs`. When nothing is published yet, or no review has
  measured the goals, say that the values appear after the person publishes
  and the first review runs (the schedule, or **Review now** on the Goals tab).
- When someone asks whether a changed rule would have helped, answer from
  Live runs (`read_live_app_runs`) or point them to Review now; never replay
  Preview or test history.

## How goals take effect

- The goals live in `.superverse/goals.md` in the application repository.
  The file is their only owner; commit it through `commit_and_push` like any
  other source.
- Pushing the file changes nothing that is measured. The platform reads it at
  the commit of the build that is Live, each time a Live deployment commits,
  so an edit takes effect when the person next publishes, and a rollback
  brings back the goals of the build it deploys. After pushing, say so: "The
  goals take effect when you publish this version."
- Each review exports the runs of the goals' Flow types from the Live engine,
  including open runs and runs that started on an earlier Live release, and
  evaluates every goal over them. Reviews run on the file's schedule and when a
  builder presses **Review now** (once a day). The Goals tab shows the values
  and the reviews on Live; on Preview it shows no values and says that goals
  are measured on Live runs.
- An invalid file never blocks a commit or a release. Once it is Live, the
  Goals tab lists its problems with their lines and reviews pause until a
  fixed file is published; **Fix with the agent** hands you those problems.

## The file

Plain prose for people, and exactly one fenced block whose info string is
`yaml goals` for the platform. The prose holds what numbers cannot: the
context, what matters most when goals pull against each other, and what is
off limits. For the template's text summary application:

````markdown
# Goals

The application summarizes texts. Every request should end with a saved
summary, quickly and at a small model cost.

```yaml goals
version: 1
review: weekly
flows: [TextSummaryFlow]
fields: [summary-status]
outcomes:
  started:
    step: PrepareSummary
  saved:
    step: SaveSummaryResult
goals:
  - id: saved-share
    kind: outcome
    statement: Most requests end with a saved summary.
    metric: rate(saved, of started)
    direction: up
  - id: duration
    kind: experience
    statement: A request ends within an hour.
    metric: p90(duration_h)
    guardrail:
      max: 1
  - id: model-calls
    kind: cost
    statement: A request calls the model once.
    metric: mean(ai.calls)
    direction: down
```
````

The block is decoded strictly: an unknown key is an error.

| Key | Rules |
| --- | --- |
| `version` | `1` |
| `review` | `weekly`, `biweekly` or `off`; `off` keeps the goals in Studio and schedules no review |
| `flows` | 1 to 4 Flow type names of the application, exactly as the Worker registers them; a review reads each FlowID's newest run |
| `fields` | At most 24 Attribute names the export may carry; only strings (cut to 256 bytes and redacted), numbers and booleans are kept, and no other Attribute, Step input or result is exported |
| `outcomes` | At most 16 named facts read from each run; each name is a lower-case slug and holds exactly one of `step: <StepType>` (the run entered it), `action: <RPC name>` (the Action was accepted) or `attribute: <name>` with exactly one of `equals: <value>` and `in: [<values>]` |
| `goals` | 1 to 8 goals |
| `goals[].id` | A lower-case slug, unique; reviews and Studio key on it, so keep it stable |
| `goals[].kind` | `outcome`, `experience` or `cost` |
| `goals[].statement` | The goal in the person's words, 1 to 200 characters |
| `goals[].metric` | The metric language below |
| `goals[].direction` / `goals[].guardrail` | Exactly one: `direction: up` or `down`, or a guardrail with exactly one of `max: <n>` and `min: <n>` |
| `goals[].target` | Optional number to reach, shown in Studio |

Prefer Step outcomes: an application can accept the same decision through
several RPCs. When a goal needs a fact the run's history does not show, such
as a result another system reports later, record it first as the
[measurable runs](backend-implementation.md#runs-that-can-be-measured-and-improved)
rules say. List `fields` to the person in plain words, since they are what a
review reads.

## Metric language

| Form | Value |
| --- | --- |
| `count(o)` | Runs where outcome `o` is true |
| `rate(o, of p)` | Among the runs where `p` is true, the share where `o` is too; none without such runs |
| `sum(f)`, `mean(f)`, `p50(f)`, `p90(f)`, `max(f)` | Over a per-run number `f`; a run without that number is left out |
| `…, where <condition>` | Inside the parentheses, restricts the runs: `where <outcome>`, `where not <outcome>` or `where field.<name> = <value>` for a name in `fields` |
| `a / b` | The ratio of two forms above; none when `b` is 0 |

Keywords take no colon (`of started`, `where not saved`), so a metric stays a
plain YAML value. The per-run numbers are `duration_h` (hours from start to
close, or to the review for an open run), `steps.<StepType>` (times the run
entered it), `actions.<RPC name>`, `wait_h.<StepType>` (hours waited in it),
`ai.calls`, `ai.input_tokens`, `ai.output_tokens`, `ai.usd` (from the model
connector Steps; none without a price for the model) and `field.<name>` for a
numeric name in `fields`. Steps, Actions and waits a run never had count as
zero. A value that rests on fewer than 20 runs is marked "few runs".

## Check a goals file

Check the file against the rules above before you commit it. The platform
reads it only once it is Live; then the Goals tab lists any problem by line.
