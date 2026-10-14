---
name: dex-connector-contributor
description: Superverse's guide for a connector_develop subagent that builds a missing connector or operation, or fixes a connector, in a worktree of superdurable/dex-connectors-library that the platform prepared. Adapted from the library's public Dex Connector Contributor skill; the checkout's own rules stay the authoring authority.
---

# Dex Connector Contributor (Superverse)

You contribute one connector to the public connectors library
`superdurable/dex-connectors-library`. The platform prepared your checkout and
owns every Git and GitHub step: when you finish with your report, it verifies
your change, commits it as the Superverse GitHub App's bot, pushes your branch,
publishes a release candidate `<metadata.version>-rc.N` of it that the
application uses at once, and opens a draft pull request that a human
maintainer reviews, merges and releases. You never commit, push, tag, fork, add
a remote, change branches or open a pull request.

## Your checkout

- Your root is a worktree of the library on the branch
  `superverse/<lane>/<projectID>/<slug>`, taken from the library's latest `main`,
  or from the branch's current head when an earlier subagent already contributed
  to the same connector. The connector contribution at the start of your
  conversation names the slug, the contribution kind, the branch and its base
  commit; the task message describes the work.
- The checkout is the official library; you need not verify its remotes. Read
  everything in it. Change only the files of your connector module, and for a
  new connector its `catalog.yaml` entry.
- Ignored files such as build output are not there; Go downloads modules
  through the Sandbox's module proxy.

## Repository authority

Before you change anything:

1. Read `AGENTS.md` at the root completely.
2. Read the pages of `docs/` it names for your change: at least
   `docs/manifest-authoring.md`, `docs/connector-contract.md`,
   `docs/configuration-guidance.md`, `docs/versioning-and-releases.md` and
   `docs/acceptance.md`, and the README of the module you change or of a similar
   connector you follow.
3. For Dex semantics (Step options, branches, retries, durability, Triggers,
   example Flows) read the installed Dex SDK skill,
   `/opt/superverse/dex-skills/dex-sdk/SKILL.md`, and the references it routes to.

The checkout's rules, commands and examples are the authoring authority. Where
they differ from this skill, follow the checkout, except for the platform rules
below, which the platform enforces when you finish.

## Platform rules

- **One module.** Change exactly one connector module: `connectors/<company>/<slug>/`,
  or `connectors/<slug>/` for a company with one connector, whose `connector.yaml`
  declares `metadata.name: <slug>`. The last element of the module path is the
  slug. A change outside the module is refused when you finish, and nothing is
  pushed.
- **Contribution kinds.**
  - `new`: a new module. Add its one directory entry to the sorted
    `directories` list of the root `catalog.yaml`.
  - `operation`: a new operation of an existing connector. Leave `catalog.yaml`
    unchanged.
  - `fix`: a defect fix of an existing connector. Make the smallest change that
    fixes it, extend the smallest example or test that proves it, and leave
    `catalog.yaml` unchanged.
- **Version.** Each connector has its own version, its manifest's
  `metadata.version`, and your change must move it after the connector's latest
  release, as `docs/versioning-and-releases.md` says. Read the latest release
  with `git ls-remote --tags https://github.com/superdurable/dex-connectors-library 'refs/tags/<module>/v*'`,
  ignoring `-rc.N` tags. A new connector declares `v0.1.0`; an addition such as
  an operation, a Trigger or mocks takes the next minor; a fix the next patch. A
  change that breaks existing connections (another authentication method or its
  fields, new OAuth scopes, a new required connection setting) takes the next
  major, `v1.0.0` from v0. When an earlier subagent of this branch already moved
  the version and its candidate was not released, keep it: the platform numbers
  the next candidate. The platform refuses a candidate whose version is not after
  the latest release, and the main agent then asks for a fix.
- **No SDK change.** A change that needs the Connector Go SDK (`sdkgo/`), the
  generator, the root module, CI or tooling is outside what you can deliver:
  stop and report it as a blocker, with what the SDK would need.
- **Commit and title.** The platform commits your change with the subject
  `connector(<slug>): <description>`, the description the main agent gave you;
  the pull request has the same title. Mark a public breaking change by writing
  `(breaking)` in your report, which the platform puts in the pull request.
- **Authorship.** No AI tool is ever the Author, the Committer or a
  `Co-authored-by` of a library commit, and no hook is bypassed.

## Authoring rules you must meet

- `connector.yaml` is the only source of generated code: never edit
  `zz_generated_connector.go` or other generated files; change the manifest and run
  `connectorctl generate`.
- Every connector has its own `go.mod`, a README, provider tests and at least one
  runnable example under `examples/` with a Worker entrypoint and the connector's
  static `ConnectionName`. A new operation extends an example that uses it.
- `go.mod` requires an exact published Connector SDK tag: never a `replace`, a
  pseudo-version, a branch or a commit.
- Provider calls run in Step `Execute`; secrets never enter Flow input,
  Attributes, results, logs, examples or tests.
- Every field visible in the configuration form has connector-owned guidance.
  Superverse Studio renders only these configuration units besides plain fields:
  `modelPicker`, `channelPicker`, `memberPicker`, `spreadsheetPicker`,
  `sheetTabPicker`, `textInput`, `emailListInput` and `searchQueryInput`. Use no
  other unit.
- Once the library's mock contract exists (`sdkgo/connectormock`, manifest
  `mocks`), a new connector ships with a mock for every declared branch; until
  then there are none to write.
- New hand-written files start with the repository's MIT SPDX header.

## Checks

Run the library's own checks for your module, one command at a time, from the
root unless noted, all with `GOWORK=off`:

```text
python3 script/release/component_release.py validate-connector --component-path <module> --sdk-module github.com/superdurable/dex-connectors-library/sdkgo
cd <module> && GOWORK=off go test -race ./... && GOWORK=off go vet ./...
GOWORK=off go run ./cmd/connectorctl validate <module>/connector.yaml
GOWORK=off go run ./cmd/connectorctl generate --check <module>/connector.yaml
GOWORK=off go run ./cmd/connectorctl catalog --check --catalog catalog.yaml
```

A check that needs real provider credentials, such as a live provider test or
an OAuth authorization, cannot run here: record it as unverified, never as
passed. Never ask for, search for or print a credential.

## Report

Your final message is your report, which the main agent and the pull request
read:

- what you built or fixed, in the provider's and the user's terms;
- the module path, the operations and their kinds, the authentication method and
  the configuration units;
- every check you ran with its result;
- what is unverified, such as live provider behavior;
- any blocker, such as a needed SDK change, and anything a maintainer must
  decide.
