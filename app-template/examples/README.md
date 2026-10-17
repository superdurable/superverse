# Examples

A new Project starts from one example of the application template: the default
`text-summary` when a person describes what to build, or the example they pick on
Studio's start page. Each example is the shared base, the files at the root of
`app-template/`, composed with one folder here:

```text
app-template/
  Makefile go.mod go.sum cmd/ internal/ scripts/ tools/ web/ ...   the shared base
  examples/
    text-summary/                 the default: its Flow, API, page, tests and AGENTS.md
    summary-approval/             extends text-summary with a human approval
    slack-thread-approval/        a Slack channel message and a thread reply
    github-releases-newsletter/   subscriptions and a Slack-started release newsletter
```

An example folder holds `example.json` and only the files that differ from what it
starts from: added files and files that replace one of the base (or of the example it
`extends`). `example.json` lists the files it `removes`. A file that the folder holds
unchanged is refused, so every folder stays a real difference.

`make generate-app-template` composes every example, checks it and writes what the
server embeds (`internal/dexai/app/apptemplate`): `app-template.tar`, the default's
composed tree, which keeps its revision `app-template@<tree>`; `examples/<id>.tar` for
each other example, whose Projects record `app-template/<id>@<tree>`; the catalog
`examples/catalog.json`; and Studio's initial design of the default. A composed tree
is exactly what a Project's first commit holds, so its Git tree ID is the one the
revision names.

## example.json

| Field | Meaning |
| --- | --- |
| `schemaVersion` | 1 |
| `id` | the folder's name |
| `title`, `category`, `apps`, `decider`, `description` | what the start page shows; `category` is one of Approvals, Finance, Support, People and Content |
| `extends`, `removes` | the example the folder starts from (the base when absent) and the files of it that the example drops |
| `requirements` | the requirements card the Project's Session records as version 1 when the example is picked: title, summary, features `R1`, `R2`, ... each with its `evidence` and either the API `tests` that prove it or an `untested_reason`, connections, user setup and acceptance |
| `design` | the card's Flow design |

`requirements` and `design` use the field names of the `confirm_requirements` card;
the generator checks the card with the Session's own card code, and that every test
and evidence file it names is in the composed tree.

## Add or change an example

1. Compose the example you start from into a directory:
   `go run ./cmd/tools/app-template-archive -compose <id> -into <directory>`.
2. Build the change there and check it with `make bootstrap check-static test-app`.
3. Copy back the files that differ into `examples/<id>/` (and list any it removes),
   write `example.json`, and keep the composed `AGENTS.md` within 6 KiB.
4. `make generate-app-template`, then `make check-app-template`, which composes every
   example and runs its `make bootstrap check-static`.
