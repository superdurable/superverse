# Version numbers

 Studio shows a version as "V*N* · *subject*", never as a commit
hash:

- The application repository's first commit is V0, the next V1, and so on.
- The subject is the first line of the commit message, cut at 72 code points with "…".
- The full hash appears only in folded technical details.

## Definition



- **Definition.** *N* is the number of ancestors of the commit, the commit itself included,
  minus 1: `git rev-list --count <commit>` minus 1. The root commit is V0.
  - Git is the only owner of the number. The repository host computes it once per record, and
    there is no counter, table or Flow state that assigns numbers.
  - The definition first asked for the first-parent position (`git rev-list --first-parent
    --count`). Both give the same number on a linear history, which is what the platform keeps;
    the reasons follow.
- **Why not first-parent.**
  - GitHub's API has no first-parent count. REST compare's `total_commits` and GraphQL
    `Commit.history.totalCount` both count every ancestor.
  - An exact first-parent position would need a history walk page by page, which does not
    scale and was declined.
  - "Ancestors minus 1" can be computed in one call on GitHub and with real `git` on the local
    Git host, so both hosts compute the same definition.
- **The platform keeps the default branch linear**, so the two definitions agree:
  - The first commit is the template's root (`internal/platform/repohost/host.go:42-46`).
  - The Agent publishes HEAD without force, and a divergence is rebased before anything is
    pushed (`internal/agents/sandbox/workspace/git/commit.go:31-39`).
  - The local Git host publishes without force (`internal/platform/repohost/localgit/host.go:275`).
  - The platform creates no merge commits.
  - Only an outside writer with repository access could add a merge. Even then, a commit's
    number never changes, and the number strictly increases along the branch; it only skips
    more.
- **Stable but gappy.** Every commit on the branch consumes a number, including commits that are
  never deployed or published. So the Release list and the timeline may skip numbers, for
  example V3, V7, V8. This is accepted.
- **Only the default branch .** The platform looks only at `main`,
  the Project's default branch, so every version has a number and there is no "unnumbered" case.
  - A Release already refuses a commit that `main` does not reach. `PrepareReleasePublishingSource`
    calls `ValidateReleaseCommit(…, "main", commit)` and rejects `repohost.ErrCommitUnreachable`
    (`internal/delivery/releases/project_release_flow_phases.go:90-97`; the contract is at
    `repohost/host.go:53-55, 96-98`).
  - Both Preview build paths resolve their commit through `ReadPreviewSourceVersion`
    (`internal/agents/sandbox/preview/preview_flow.go`): a manual build inspects clean workspace
    HEAD; `deploy-preview` uses its pinned pushed commit. `readPreviewSourceVersion` and the final
    `registerPreviewAppManifest` (`preview_configuration.go`) each call
    `ValidateReleaseCommit(…, "main", commit)` before describing that commit. A manual start
    verifies and reuses its already READY source.
    `manifestFailureCode` (`preview_failure.go`, end of file) maps
    `repohost.ErrCommitUnreachable` to the existing `PREVIEW_HEAD_NOT_READABLE`, as it already
    maps `repohost.ErrNotFound`. The code's message is reworded to "Push the current Sandbox
    commit to main, then build processes again".
  - A commit that `main` does not reach, such as one a Sandbox shell pushed to another branch, is
    therefore refused before anything is prepared or numbered.
  - **Flow contract delta.** The metadata Step and its existing refusal route are specified by
    [Automatic process builds](auto-refresh.md#steps-rpcs-and-recovery).

## Computed once, frozen with the record

A new host method returns the version of a commit:

```go
// DescribeCommit returns the version of a commit that the caller already checked is on main: its
// number and its subject. A commit's answer never changes, so callers freeze it with their record.
DescribeCommit(ctx context.Context, fullName, commit string) (CommitVersion, error)

type CommitVersion struct {
    Number  uint64
    Subject string // first line, at most 72 code points
}
```

`CommitVersion` lives in `internal/platform/repohost`. Each host computes it as described above:

- local Git: `git rev-list --count` plus `git log -1 --format=%s`;
- GitHub: one GraphQL query for the history count and the message.

Every caller uses the same branch, `main`, as the Release does: `ListReleaseCandidates`
(`internal/api/release.go:47`), the Release check and the Preview check.

The number belongs to the Git commit and is copied into these records:

| Record | Where it is computed | Carried by |
|---|---|---|
| Release | `PrepareReleasePublishingSource` (`internal/delivery/releases/project_release_flow.go:451`), which already holds `repohost.Host` (`:52`) | `PreparedReleaseSource.Version`. This is persisted in the existing `releases.prepared_source` JSONB column (`internal/platform/postgres/migrations/0002_read_model.sql:41`), so **migration 0006 needs no column**. Deployments, the Live pointer and the timeline read it through the release they name. |
| Preview source | `ReadPreviewSourceVersion` publishes the name before compilation; `PreparePreviewSource` reads it again when registering the final manifest, through `PreviewConfiguration.host` | `PreparedPreviewSource.Version`, then `PreviewState.Version` when it serves (`serve` in `preview_snapshot.go`), and each `RecentStarts` entry's `Version` |
| Release candidates | `ListReleaseCandidates` (`internal/api/release.go:34-55`), for each listed commit | The response only. Answers are cached in process by commit SHA, at most 1024 entries; a commit's answer never changes. This is a per-request server read, never a per-render one. |

- Open records created before this change have no `Version`. The release projection's JSONB
  lacks the field, and Studio shows "Version not recorded" with the hash folded. A Release or
  Preview prepared again records it.
- A Preview deployment queued before preparation shows "the latest push", without a number. Its
  version appears when `ReadPreviewSourceVersion` commits, before process compilation. Attribute writes
  within a Step are staged until invocation success, so the name needs this separate metadata boundary.
- **Flow contract delta.**
  - The value fields are additive and use `json:",omitzero"`, with no new secret or index.
    Preview serializes them under its existing command admission; the added metadata Step
    commits the intermediate name before compilation.
  - Git reads are idempotent and retry safe; Preview's deadlines, retries, typed refusals and
    compatibility are specified by [Automatic process builds](auto-refresh.md#steps-rpcs-and-recovery).

## Protobuf fields (in SETUPAPI)

```proto
// A commit as Studio shows it: "V{number} · {subject}".
message CommitVersion {
  uint64 number = 1;
  string subject = 2;
}
```

| Message | New field |
|---|---|
| `ReleaseCandidate` | `CommitVersion version = 4` |
| `PreparedReleaseSourceSnapshot` | `CommitVersion version = 7` |
| `ProjectLive` | `CommitVersion version = 7` (from the Live Release's prepared source) |
| `DeploymentSnapshot` | `CommitVersion version = 13` (from its Release) |
| `PreparedPreviewSourceSnapshot` | `CommitVersion version = 9` |
| `PreviewSnapshot` | `CommitVersion serving_version = 20` (`PreviewState.Version`) |
| `PreviewStartRecord` (new in SETUPAPI) | `CommitVersion version` |
| `EnvironmentEvent` (new in SETUPAPI) | `CommitVersion version` |

The Session's delivery notification shows its commit
(`components/agent/conversation/ConversationMessage.tsx:93`). It is filled from the Preview's
prepared source version when the deployment's outcome is reported. That is one optional field,
`version`, on the notification's snapshot message, and it is also in SETUPAPI.

## Studio

`lib/shared/format.ts`'s `shortCommit` is replaced by `versionLabel(version, commit)`, in a new
directory, `lib/shared/version/`, because `lib/shared` is full. It
renders "V12 · Add refund approval", or "Version not recorded". The hash
goes only into folded technical details.

Every surface that shows a hash today switches:

- `components/build/PreviewAutoDeploy.tsx:14-20, 100-107`
- `components/build/PreviewMenu.tsx:29-30`
- `components/build/FlowSurface.tsx:44`
- `components/project/ProjectBuilder.tsx:215`
- `components/project/ProjectHome.tsx:25`
- `components/project/VersionComparison.tsx:64`
- `components/publishing/PublishingPanel.tsx:160`
- `components/agent/conversation/ConversationMessage.tsx:93`

So do the new surfaces: the environment tabs, the Publish popover, the timeline, recent events, the
rollback and Stop Live confirmations, and Runs' version note. A lint rule in
`eslint.config.mjs` forbids `.slice(0, 12)` and `.slice(0, 7)` on fields named `*commit*` or
`*Commit*` outside `lib/shared/version/`.

## Roadmap

| Line | Part |
|---|---|
| SETUPAPI | `CommitVersion` and the fields above, with the mock fixtures carrying versions |
| APPLY | `DescribeCommit` (both hosts, with a Dex integration test on Local Native using the local Git host); the Preview's `main` reachability check; the Preview's `Version` in `PreparedPreviewSource`, `PreviewState` and `RecentStarts`. APPLY is first, so it owns the host method. |
| LIVEDRAFT | No part |
| PUBGATE | The Release's `PreparedReleaseSource.Version`, and the candidates' versions |
| GATE | `lib/shared/version/` and the existing surfaces listed above |
| WIZARD, TIMELINE | Their new surfaces |

**Tests.**

- A Dex integration test on the local Git host creates the template root (V0) and two Agent
  pushes (V1, V2). It checks that the Preview and a Release record those numbers, and that a
  commit pushed only to another branch is refused by the Preview with
  `PREVIEW_HEAD_NOT_READABLE`.
- The GitHub host's query is covered by the GitHub integration tests, which run only when the
  platform developer asks.
- `create-project.spec.ts` asserts that the Publishing list shows "V1 ·".
