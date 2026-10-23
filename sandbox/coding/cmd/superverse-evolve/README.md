# superverse-evolve

`superverse-evolve` is the agent's measurement in an agent review: it checks a goals file, and
evaluates, compares and gates a proposed change over the review's folder that
`place_evolve_export` placed, the review's export of the Project's Live, published runs. The
platform never runs it; the main agent and its `investigate` and `change` subagents run it with
`bash`. It is the same code as `dexai evolve` (the goals evaluator and the run record of
`internal/dexai/delivery/evolve/goals` and `runexport`), without `export` and without any way to
read other runs or goals: only the Live runs of the review count, and the goals are the Live
build's.

```sh
superverse-evolve check [file]
superverse-evolve metrics --review <id>
superverse-evolve compare --review <id> --before <file> --after <file> [--strata <f1,f2>] [--target <goal id>] [--before-commit <commit>] [--out <file>]
superverse-evolve open-runs --review <id> --before-defs <dir|file> --after-defs <dir|file> [--out <file>]
```

| Command | Does |
| --- | --- |
| `check [file]` | Parses and validates a goals file (default `.superverse/goals.md`) and prints every problem with its line; a `within` under an hour, which only test profiles accept, is a warning on standard error. It measures nothing |
| `metrics --review <id>` | Evaluates every goal of the folder's `goals.md` over its `runs.jsonl` at `metrics.json`'s `evaluated_at`, as `metrics.json`; it reproduces the review's values |
| `compare --review <id> ...` | The backtest comparison of the before and after decision lines over the folder's runs, as `backtest.json`, evaluated at `metrics.json`'s `evaluated_at`, so its before values are the review's |
| `open-runs --review <id> ...` | The open-run gate: the Live build's and the proposal's Flow definitions against the open runs' `active_steps`, as `open-runs.json` |

## The review's folder

`--review <id>` reads `.local/superverse-evolve/<id>/` under the root of the Git repository of the
current directory: `/workspace` for the main agent, and a subagent's worktree, where `start_agent`
`inputs` link the folder at the same path. The folder holds `runs.jsonl`, `manifest.json`,
`metrics.json` and `goals.md`. The command refuses, with exit code 1, a folder:

- whose manifest names no source, a source other than `live`, no Project, or another review;
- whose manifest names no goals commit or no `runs_sha256`;
- whose `runs.jsonl` does not have the SHA-256 `runs_sha256` names;
- whose `goals.md` the parser refuses, or whose `metrics.json` names no evaluation time.

It never reads the workspace's `.superverse/goals.md` for a review: that file is a draft until it
is published. These checks catch mistakes; the review's own report (`report_evolve_review`, then
EvolveFlow's `ReportReview`) is the gate, which checks the binding below against the review.

## Binding and gates

`compare` and `open-runs` write an `export` binding into their output: `review_id`,
`goals_commit`, `runs_sha256` and `records` from the folder, `before_commit` (the Live build commit
the before side's code came from: `--before-commit`, by default the goals commit) and `base_commit`
(`git rev-parse HEAD` of the current directory, a subagent's worktree base, since subagents never
commit). `compare` gives each goal the digest of its definition. A gate that fires is printed on
standard error; a gate that refuses the proposal makes the command exit 1 after writing its output.

`compare` reads the decision lines a backtest program writes, one JSON line per run, as
`dexai evolve compare` does: `{"flow_id", "decisions", "points", "numbers"}`.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Done; a gate that only warns is on standard error |
| 1 | An error, a refused folder or a gate that refuses the proposal, on standard error |
| 2 | A usage error: an unknown command (such as `export`) or flag (such as `--runs`, `--goals` or `--dex`), or a missing required flag |
