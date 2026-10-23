# Goals and their measurement

Use this reference when the person asks to set, change or fix the
application's goals, when Studio's Goals tab hands you "Help me set goals for
this app" or the problems of a goals file, or when someone asks how the
application is doing against its goals, and when an `evolve` notification
asks you to review them. It covers the goals file, the metric language, how
the platform measures them and the agent review's procedure.

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

Check the file against the rules above before you commit it, with
`superverse-evolve check` in the Sandbox (every problem with its line). The
platform reads it only once it is Live; then the Goals tab lists any problem by
line.

## Agent reviews

On the platform, each review of the goals that a builder's Project has agent
reviews for waits for you: an `evolve` notification (phase `review`) names the
review `r-N`, the window, the run count, the goals commit (the Live build's),
each goal's value and change, and the newest proposals with their decisions. It
arrives only while you are idle and opens its own turn. Within 24 hours,
report the review once, with at most one proposal; otherwise it expires.

Only runs of the Live, published process count (above). In a review that means
the folder `place_evolve_export` places and nothing else.

### The procedure

1. Call `place_evolve_export` with the review ID. It places, read-only, in
   `.local/superverse-evolve/<review>/`: `runs.jsonl` (the review's export of the
   Live runs, one run per FlowID, newest first), `manifest.json` (with
   `runs_sha256` and `goals_commit`), `metrics.json` (every goal's value as the
   review measured it) and `goals.md` (the goals of the Live build). Read
   `goals.md` and `metrics.json`. The workspace's `.superverse/goals.md` is a
   draft and never feeds a review. The folder never enters the repository:
   `commit_and_push` refuses it.
2. Discard the results of earlier proposals that were declined, superseded or
   stale and are still open (`merge_agent_changes discard`), and report each
   with `report_evolve_review` phase `discarded`.
3. Start an `investigate` subagent with `inputs: [".local/superverse-evolve/<review>"]`,
   which links the folder read-only at the same path in its worktree. Give it
   the goals, the metrics, the past decisions and the rule that only this
   folder counts. It reports at most five findings, each with the goal it bears
   on, the segment, the numbers (from scripts over `runs.jsonl` or
   `superverse-evolve`, never estimated by eye), the run count, three example
   FlowIDs and whether a change could be checked exactly.
4. If a finding is worth a change that keeps the guardrails and the off-limits
   prose of the placed `goals.md`, start one `change` subagent with the same
   input. It makes the change, runs the backtest and the open-run check below
   and writes `.superverse/evolve/<review>/proposal.md`, `backtest.json` and
   `open-runs.json`, tracked paths that are committed with the change. It never
   edits `.superverse/goals.md`, keeps the Dex Go SDK at or above the Live
   release's, and does not run the application's tests: the commit gate runs
   them when the approved change is pushed.
5. Call `report_evolve_review` with phase `reported` and the verdict:
   `proposal` with the change subagent's `agent_id` and a `title` in the goal's
   words (and `kind` `trial` when a gate requires one), or `no_change`. Put
   findings without a proposal in the `summary` as noticed. Then send one short
   chat message: the verdict, and that the proposal waits on the Goals tab.
   Never paste the proposal into the chat.
6. On the `evolve` notification of phase `decision`: for an approval,
   `merge_agent_changes apply` the change subagent's result, resolve conflicts
   with the work done since, `commit_and_push` (the Preview deploys it), then
   `report_evolve_review` phase `applied` with the pushed commit. For a decline,
   `merge_agent_changes discard` and phase `discarded`. You never publish: the
   person publishes from the Publish popover.

### The backtest

Decision replay: the application's own decision code, before and after the
change, over the review's Live runs. A decision that sits inside a Step moves
first, without a change of behavior, into a plain function of the run's
recorded facts ([measurable runs](backend-implementation.md#runs-that-can-be-measured-and-improved)).

- **Before** is the Live build's code: the folder's manifest names its
  `goals_commit`. Take it from `git archive <goals_commit>` into a directory of
  `/tmp`, and compare it with your worktree's base (`git diff <goals_commit> HEAD
  -- <the files the change touches>`); when they differ, the "before" side still
  comes from the Live commit, and the proposal's Risks name the unpublished
  difference. **After** is the change's tree.
- Write a small Go program under `.superverse/evolve/<review>/backtest/` that
  reads `runs.jsonl` and, for each run, calls the decision functions with the
  run's recorded facts and writes one JSON line:
  `{"flow_id", "decisions": {"<name>": <scalar>}, "points": {"<name>": {"step":
  "<Step>", "entry": <n>}}, "numbers": {"steps.<Step>": <n>, "ai.calls": <n>, ...}}`.
  A point is the entry into the Step where the run takes the decision. Give
  every number a goal's metric reads, so a changed run is known exactly. Run it
  with `go run .superverse/evolve/<review>/backtest/main.go <runs> <out>` in
  both trees and write the lines under `.local/`, never under `.superverse/`.
- `superverse-evolve compare --review <review> --before <before lines> --after
  <after lines> [--strata <fields>] [--target <goal>] --out
  .superverse/evolve/<review>/backtest.json` labels every run `same`,
  `changed_exact`, `changed_estimated` or `unknown`, checks the calibration
  (the "before" numbers against history, at least 98%) and evaluates every goal
  at the review's own time, so its "before" values are the review's.
- `superverse-evolve open-runs --review <review> --before-defs <defs> --after-defs
  <defs> --out .superverse/evolve/<review>/open-runs.json` compares the Live
  build's and the change's Flow definitions
  (`python3 scripts/render-flow-definitions.py --output <file>` in each tree)
  with every open run's current Steps: a Step an open run is in that the change
  removes or renames, or an Attribute whose value type changes, refuses it.
- Both write an `export` binding: the review, the goals commit, the runs'
  SHA-256 and count, the before commit and your base. `superverse-evolve`
  refuses a folder whose manifest is not the review's Live export or whose
  `runs.jsonl` does not match its digest, and has no command that reads other
  runs: `export`, `--runs`, `--goals` and `--dex` are not commands of it.

The review refuses a report, without storing a proposal, with a code:

| Code | When |
| --- | --- |
| `stale_review` | The review no longer waits: it ended, expired or was skipped |
| `stale_proposal` | An `applied` or `discarded` report that does not follow the proposal's state |
| `invalid_backtest` | A file is missing or does not decode, the calibration is under 98%, a guardrail breaks over the runs known exactly, an open run blocks the change, or a trial is required (the gate is in the message) |
| `export_mismatch` | `backtest.json` or `open-runs.json` is not bound to this review's placed export, before commit and goals: another review's folder, edited runs, the workspace's goals or a left-out goal |
| `evolve_result_too_large` | `proposal.md` over 32 KiB, `backtest.json` over 256 KiB or `open-runs.json` over 64 KiB |

### The proposal

`proposal.md` carries aggregates only, no FlowIDs and no per-run values, since
it is committed: **Title** (one line in the goal's words), **Why** (the
findings, each with its numbers and run counts), **What changes** (in plain
words, then the files), **Goals** (each goal before and after, known and
estimated, and the guardrails), **Backtest** (the run labels, the calibration,
the estimation's method and sample sizes), **Open runs** (the open runs by
current Step and what the change does to each), **Risks** (what could go wrong,
what the next review watches, the off-limits list and that nothing in it
changed) and **Undo** (roll Live back to the release before; the next review
proposes the code revert). The numbers come from `backtest.json` and
`open-runs.json`; Studio shows them from those files, never from your text.
