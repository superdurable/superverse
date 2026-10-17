# Examples

A new Project starts from one example of the application template: the example a
person opens on Studio's start page, the example the server finds clearly closest to
what they describe (when its `agent.closestExamplePick` is on), or else the default
`text-summary`. Each example is the shared base, the files at the root of
`app-template/`, composed with one folder here:

```text
app-template/
  Makefile go.mod go.sum cmd/ internal/ scripts/ tools/ web/ ...   the shared base
  examples/
    text-summary/                 the default: its Flow, API, page, tests and AGENTS.md
    summary-approval/             extends text-summary with a human approval
    slack-thread-approval/        a Slack channel message and a thread reply
    github-releases-newsletter/   subscriptions and a Slack-started release newsletter
    search-words.json             the search's stop words and synonyms
    facets.json                   the ideas a request can mention, and the negators
    labelled-requests.json        requests with the example each starts from
```

An example folder holds `example.json` and only the files that differ from what it
starts from: added files and files that replace one of the base (or of the example it
`extends`). `example.json` lists the files it `removes`. A file that the folder holds
unchanged is refused, so every folder stays a real difference.

`make generate-app-template` composes every example, checks it and writes what the
server embeds (`internal/dexai/app/apptemplate`): `app-template.tar`, the default's
composed tree, which keeps its revision `app-template@<tree>`; `examples/<id>.tar` for
each other example, whose Projects record `app-template/<id>@<tree>`; the catalog
`examples/catalog.json`; the search index `examples/example-search.json`, which Studio
bundles byte for byte (`web/studio/lib/generated/example-catalog/`); the decision's
rules `examples/example-match.json`; and Studio's initial design of the default. A
composed tree is exactly what a Project's first commit holds, so its Git tree ID is the
one the revision names. When an example's tree differs from the one `main` holds, the
generator keeps `main`'s archive as `examples/previous/<id>.tar`, one per example, the
default's included, and a removed example keeps its last archive there, so that a
Setup a newer server runs for a revision an older one accepted during a rollout still
finds its tree. It reads `main` at the merge base of `HEAD` and `origin/main` (in a
merge commit without `origin/main`, its first parent), so every commit of a branch
keeps the same tree, and `make check-app-template` fails when the committed
`examples/previous/` differs: commit it with the archives.

The coding image prewarms the module graph of every example the catalog lists and
compiles each composed tree into its seeded Go build cache, so a Project's first
`make bootstrap` and first commit gate reuse its example's compiled dependencies
whichever example it starts from ([the coding image](../../sandbox/coding/README.md#toolchain-and-caches)).
Every example grows that seed by the packages no earlier example compiled: its own,
those of the modules the base does not compile (its connectors and their
dependencies), the packages of shared modules that it imports and the base does not,
and every shared module again at each release it selects besides the base's. Selecting the
base's release of each module it shares with the base spares only that last part:
a connector example still adds its connector's packages, about 7 MB for Slack. An
example with its own `web/package-lock.json` adds the npm packages no other lockfile
installs. The image build fails when the seeds pass their 1 GiB budget
([seed budget](../../sandbox/coding/README.md#seed-budget)).

## example.json

| Field | Meaning |
| --- | --- |
| `schemaVersion` | 1 |
| `id` | the folder's name |
| `title`, `category`, `apps`, `decider`, `description` | what the start page shows; `category` is one of Approvals, Finance, Support, People and Content |
| `extends`, `removes` | the example the folder starts from (the base when absent) and the files of it that the example drops |
| `requirements` | the requirements card the Project's Session records as version 1 when the example is picked: title, summary, features `R1`, `R2`, ... each with its `evidence` and either the API `tests` that prove it or an `untested_reason`, connections, user setup and acceptance |
| `design` | the card's Flow design |
| `match` | what a request must mention for the server to start from the example: `core`, the facets it needs every one of; `signals`, facets that count for it; `conflicts`, facets that rule it out (see [Closest example](#closest-example)) |

`requirements` and `design` use the field names of the `confirm_requirements` card;
the generator checks the card with the Session's own card code, and that every test
and evidence file it names is in the composed tree.

## Closest example

The same search ranks the examples in Studio's grid while a person types and shortlists
them on the server when they press Build. It is generated from each example at catalog
build, so nobody writes search words: the title and the apps count three times, the
category, the decider, the description and the card's feature texts once. A word is a
run of ASCII letters and digits, lower-cased, with its endings cut ("approvals" and
"approve" meet), and the stop words of `search-words.json` count for nothing. Its
synonym groups make a paraphrase meet the example ("sign off" counts as "approve",
"Gmail" as "email"); keep the list short. The generator scores every term of every
example with BM25, so a word few examples hold counts more than one many share, and
writes one inverted index; ties keep the catalog's order. A text in another language
than English ranks nothing and starts from the default.

The server then decides from the search's best five, without a network call or a
model. `facets.json` declares each idea a request can mention, with its English terms
(`*` ends a prefix), its `paired` terms, which mention it only in a sentence that also
holds one of its `companions` ("thread" with "reply", so archiving Slack threads is no
thread reply), a label, the facets mentioning it `implies` (an approval is a wait for
someone's answer), and the negators. Sentences end at `. ! ? : ;` before white space
and at a line break. In a sentence, a term after its first negator negates its idea;
when no term follows the sentence's last negator ("Slack is not used", "AI isn't
allowed"), every idea of the sentence is negated; and a heading of at most four words
whose negator ends it ("Not needed:") negates the next sentence. Negation errs toward
the default. An example is eligible when the request mentions every `core` facet, no
`conflicts` facet, no negated facet of its block or connectors, and scores at least 3:
3 per core facet, 1 per signal, less 2 per connector of the example the request
neither mentions nor needs, and less 3 per core facet of another eligible example
that its block lacks (unless one extends the other), so a request that combines two
examples starts from neither. The eligible example with the highest score, then the
fewest Steps, then catalog order, wins when it is the only one, extends the runner-up
or is extended by it, or leads it by 2; otherwise, when the default wins, when the
request names the default's own Flow or Step types or Flow file, and when it holds a
letter of a script other than Latin, such as Chinese, the Project starts from the
default.

Write an example's `match` block from its card and review it:
`go run ./cmd/tools/app-template-archive -propose-match <id>` proposes the facets of its
title as the core and those its apps, trigger, decider and description mention as
signals. Every connector of the example needs a facet named after it.

`labelled-requests.json` holds about 140 requests, each with the example it must start
from or `default`: direct and paraphrased requests, negated ones, near misses, unrelated
ones, a few mixing Chinese with English names, the start page's placeholders and the
first messages of the tests that create Projects. The generator decides every one and fails on a
different pick, so adding an example cannot quietly change the picks of another;
Studio's check (`web/studio/scripts/check-example-search.mjs`) proves the browser ranks
every one as the server does and that each expected example is in the grid's top six.
Every new example adds a few requests; someone other than the example's author writes
its near misses. Its picks measure the rules only on requests their authors have seen:
it is a regression set, not a measure of how often they pick wrongly. The mock model's
loader also requires the first message of every scenario without a workspace to start
from the default; a scenario that declares `start: matched` needs the server to match
its first message to its workspace, and an opened example's scenario is created from
that example, not from its message.

## Add or change an example

1. Compose the example you start from into a directory:
   `go run ./cmd/tools/app-template-archive -compose <id> -into <directory>`.
2. Build the change there and check it with `make bootstrap check-static test-app`.
3. Copy back the files that differ into `examples/<id>/` (and list any it removes),
   write `example.json` with its `match` block, keep the composed `AGENTS.md` within
   6 KiB, and add labelled requests for it, its near misses by someone else.
4. `make generate-app-template`, then `make check-app-template`, which composes every
   example and runs its `make bootstrap check-static`.
