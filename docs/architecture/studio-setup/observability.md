# Observability

## Environment status

Each environment tab of the bar shows exactly one status: a dot, and a word only when something is
abnormal. The word links to the relevant fix. Studio derives
the status from facts that already exist, and the status set is a closed set declared once in
`lib/project/setup/environment-status.ts`, with each value's word, tone and fix; it returns
`{kind, tone, word, fix, detail, problem}`. When several apply, the first in table order wins.

Colours follow the owner's E3: Preview's normal dot is grey and only a running Live is green. The
process picker's pill and the active tab's underline take the same environment colour.

**Preview** (the "Preview" tab):

| Status | When | Dot, word and fix | Source |
|---|---|---|---|
| `FAILED` | `PreviewSnapshot.failure` is set, and the last `recent_starts` entry is `FAILED` or `REFUSED` | red, "failed"; the problem panel in the Interface pane, with Fix | `failure`, `recent_starts` |
| `STOPPING` | `status` is `STOPPING` | pulsing, "stopping"; red, "stop failed", when its cleanup failed, with the Interface pane's retry | `status`, `failure` |
| `STARTING` | `status` is `STARTING`, or the automatic deployment is `RUNNING` | pulsing, "starting", or "updating" for the automatic deployment of a push; the Interface pane, which shows "step k of 5" with the phase label and elapsed time | `phase_timings` (the phases `configuration`, `environment`, `grant`, `application`, `route`; `preview_snapshot.go:72-81`) |
| `MISSING` | The configuration's `status` is not `READY` and the gap list is not empty | amber, "needs setup"; Setup, focused on the first gap | `GetProjectConfiguration.status` and `requirement_gaps` |
| `CANNOT_CHECK` | The `status` is not `READY` and there are no gaps: a connector release that cannot be resolved, an unpublished trigger, a digest conflict | amber, "cannot check setup"; the problem panel with the reason from `validation_errors` | `GetProjectConfiguration.status`, `validation_errors` |
| `UNAPPLIED` | `unapplied_change_count > 0` | grey, no word; Setup's Preview state says "Updating Preview with N changes…" while it applies them on its own, and the Tools tab carries a badge | `GetProjectConfiguration` |
| `RUNNING` | `status` is `READY` | grey, no word; the tab's hint names the version | `source_commit` |
| `STOPPED` | otherwise | hollow, "stopped", or "not started" before a first start; the word opens Start Preview, which Runs, Inbox and the Interface also offer while they are empty | `status` |

Setup's automatic apply and the Publish popover's Publish act only
when the configuration's `status` is `READY` (review 2), never on an empty gap list alone; for
publishing it is the next publish of the chosen version. A non-READY configuration without gaps shows "Cannot check: {reason}".

A failed start never shows *waking*. `FAILED` outranks `STARTING`, because `finishCommand`
records the failure and ends the phase (`preview_deployment.go:260-274`), and the next admitted
start clears it (`preview_command.go:205-210`).

**Live** (the "Live" tab):

| Status | When | Dot, word and fix | Source |
|---|---|---|---|
| `PUBLISHING` | An active build or deployment | pulsing, "publishing"; Tools ▸ Publishing with the [stage bar](#publishing-stages) | `ProjectSnapshot.active_deployment`; the Release and Deployment Watch `phase_timings` |
| `FAILED` | The newest build or deployment of this publish failed; a CANCELLED one is not a failure | red, "publish failed"; Tools ▸ Publishing with the problem panel | `ReleaseBuildRevisionSnapshot.failure`, `DeploymentSnapshot.failure` |
| `RUNNING` | Live serves | green, no word; the Publish button counts the versions ahead ("N new") and, on the Live tab, the waiting settings ("N waiting") | `ProjectSnapshot.live`, the release candidates, the Live item list |
| `NOT_PUBLISHED` | No Live yet | the Live tab is disabled, "Live appears with the first publish" | `ProjectSnapshot.live` absent |

Operators see only the Live tab, as before.

## Error contract

There is one problem format everywhere, `components/ui/problem/ProblemPanel.tsx`, with four
parts:

1. **What happened**: the operation and its phase, in plain words.
2. **Why**: the server's message for the code.
3. **How to fix**: one button, chosen by `remedy`.
4. **Technical details** (folded): the code, the phase, the truncated cause, and the request
   time.

There are no bare codes and no "did not complete".

The typed server contract extends `Failure` (`superverse.proto:64-69`) with two enums:

```proto
// What the reader can do about a failure; declared with each failure code.
enum FailureRemedy {
  FAILURE_REMEDY_UNSPECIFIED = 0;
  FAILURE_REMEDY_OPEN_SETUP = 1;          // complete or fix Setup items (gaps listed)
  FAILURE_REMEDY_APPLY_AGAIN = 2;         // start Preview again / Apply
  FAILURE_REMEDY_ASK_AGENT = 3;           // the application's source must change
  FAILURE_REMEDY_REAUTHORIZE = 4;         // a connection needs authorization
  FAILURE_REMEDY_FREE_WORKSPACE = 5;      // workspace volume full
  FAILURE_REMEDY_PUBLISH_AGAIN = 6;       // retry the build or deployment
  FAILURE_REMEDY_CONTACT_OPERATOR = 7;    // platform defect or terminal state
}
// Who must act: the application's source, the user's configuration or the platform.
enum FailureOwner {
  FAILURE_OWNER_UNSPECIFIED = 0;
  FAILURE_OWNER_APPLICATION = 1;
  FAILURE_OWNER_CONFIGURATION = 2;
  FAILURE_OWNER_PLATFORM = 3;
}
```

One owner for each part of the contract (review 8):

- **Owner.** There is one Go closed set, `conversation.DeliveryOwner`
  (`internal/dexai/agents/session/conversation/notification.go:256-260`), which PreviewFlow already
  uses. `releases.FailureOwner` (`internal/dexai/delivery/releases/delivery_feedback.go:36`) is replaced
  by it in REMEDY: `releases` sits at a higher level than `conversation`, so it may import it. The
  mapping `deliveryOwner` (`internal/dexai/agents/session_flow_service_feedback.go:135`) then
  disappears. `boundary.Enum` maps the one set to the protobuf `FailureOwner` once, and
  `make check-proto-enums` proves coverage.
- **Remedy.** The remedy is declared only on the server, beside each failure code's message:
  - PreviewFlow: `defineFailure` (`preview_failure.go:39-47`).
  - The release and deploy failure codes, in their packages' code tables.
  - The ErrorInfo reasons Studio shows, such as `REVISION_CHANGED`, `PREVIEW_VALIDATION_REQUIRED`
    and `RELEASE_CONFIGURATION_STALE`, and under 0027 the refusals of a cancel that came too late,
    `RELEASE_BUILD_NOT_CANCELLABLE`, `RELEASE_BUILD_DEPLOYING` (remedy `CANCEL_DEPLOYMENT`, with the
    deployment in the ErrorInfo metadata `deploymentId`) and `DEPLOYMENT_NOT_CANCELLABLE`.
    `RELEASE_CONFIGURATION_CONFLICT` went away with conflicts.
    `CONFIGURATION_ITEM_CANNOT_FOLLOW` is not an ErrorInfo reason: the configuration host answers it
    as a 409 code in the Flow API document, and Studio says why in the control's menu note. These
    carry the remedy in the ErrorInfo metadata `remedy`, typed on the server.

  Studio never chooses a remedy from a code.
- **Copy.** Messages that name removed UI are rewritten in the same pull request: "Open Build
  Configuration" (`PREVIEW_CONFIGURATION_FAILED`) becomes "Complete Setup · Preview, then
  Apply", and the release gap copy (`project_release_flow.go`) says "in Setup · Live", the Live
  tab's Setup, which under 0027 shows the same next-publish settings as the Next publish column.
- **Studio copy** has one module, `lib/shared/problem/`. It holds:
  - the button label and action of each remedy, as `Record<FailureRemedy, …>` over the
    generated enum, so the compiler enforces coverage;
  - the phase-to-"what happened" labels;
  - the gap phrases. `GAP_PHRASES` moves here from `lib/project/requirement-gaps.ts:4`.
  - `problemOf(failure | ApiError)`, which builds the four parts.

  An unknown reason without a remedy falls back to "Something went wrong on the platform" with
  `CONTACT_OPERATOR` and the raw details folded.
- **Raw logs.** The Preview's logs stay where they are today (the Session's Preview log tool).
  The panel links to them folded. A publish's build log is read inline in Tools ▸ Publishing
  ([build log](#build-log)).

## Environment timeline

`ProjectService.ListEnvironmentEvents(project_id, scope, page_size ≤ 50, page_token)` returns
`EnvironmentEvent{kind, occurred_at, actor_account_id, actor_display, commit, outcome, failure,
phase_timings, release_id, deployment_id, build_revision_id, configuration_revision,
preview_skip_reason}`, newest first.

`kind` is a closed enum, and every kind is derived from existing state:

| Kind | Scope | Derived from |
|---|---|---|
| `PREVIEW_CONFIGURATION_APPLIED` | Preview | A `RecentStarts` entry with `Applied` (actor, revision, time) |
| `PREVIEW_STARTED` | Preview | A `RecentStarts` entry without `Applied`; the actor is empty for an Agent push ("the agent deployed {commit}") |
| `PREVIEW_START_RESULT` | Preview | The same entry's `Outcome`, `EndedAt`, `FailureCode` and `Timings` (merged into its start row in Studio) |
| `PUBLISHED` | Live | `release_build_revisions` with `auto_deploy`, joined with its `deployments` row: actor, preview validation or skip reason, outcome |
| `DEPLOYED_EXISTING_VERSION` | Live | A `deployments` row whose `start_identity` request is not a build's automatic deployment. It is shown as "rolled back to {version}" when its Release's `created_at` is older than that of the Live Release it replaced. Commits are not compared by SHA |
| `PUBLISH_CANCELLED` (0027) | Live | A `release_build_revisions` row or a `deployments` row whose status is `CANCELLED`, with its `cancellation` column: who cancelled and when, and the stage it stopped in from its `phase_timings`. A publish whose automatic deployment was cancelled is listed once, as `PUBLISH_CANCELLED` instead of `PUBLISHED`; a cancelled rollback is listed the same way |
| `LIVE_REMOVED` | Live | Not available. `RemoveLive` keeps no history beyond the Live pointer, so it stays out until ProjectFlow records it ; Live history then names who stopped Live |

**Paging.**

- Preview reads one `GetPreview` call and returns at most 50 entries, the bounded history. Older
  starts are not kept, by design.
- Live pages the SQL rows by `created_at DESC` with the keyset token, as the other read-model
  lists do.

**Authorization.**

- Preview needs builder.
- Live needs operator, because operators see Live.

**Actor names.** Actor display names come from `accountdao`. An account that is no longer a
member shows as "a former member".

There is no new table and no Dex history read: Dex history stays diagnostic only. Live's events
are the Live history of Tools ▸ Publishing and a folded History on the Live Interface; the bar's
`LiveTimelineMenu` is gone.

## Recent events received

### Placement and bounds

`internal/dexai/tenancy/slackrelay/recent_events.go` is a new file, the package's fifth non-test file.

- `recentEvents` holds a ring of 50 entries per scope (`projectID/scope`), for at most 512
  scopes. The least recently written scope is evicted first.
- **Byte budget** (review 9). Every string field is truncated to 128 bytes at a UTF-8 boundary,
  so an entry is at most about 1 KiB and the buffer at most about 25 MiB.
- `buffer_started_at`, the time this process's buffer started, is in every response. Studio
  says "events since {time}" so that an empty list after a restart is not read as "no events".
- One mutex guards it, as `tokenCache` does (`delivery.go:94-138`).
- It is memory only and cleared on restart (the owner's "relay is not persisted"), and it is per
  process.

### Entry

```go
type RecentEvent struct {
    ReceivedAt     time.Time
    EventID        string // Slack's event_id
    EventType      string // inner event.type, e.g. "message"
    EventSubtype   string // inner event.subtype, empty for a plain message
    TeamID         string
    ChannelID      string // inner event.channel
    ConnectionName string
    Fate           EventFate
    HTTPStatus     int    // the application's answer, 0 without one
    Duration       time.Duration
}
```

- **Metadata only.** Reading `type`, `subtype` and `channel` is new parsing of the inner event,
  with a three-field struct that never reads text, user, files or blocks.
- The body bytes are never stored.
- No token or signature is stored.

`EventFate` is a closed set declared once with each value's label:

| Fate | When | Label |
|---|---|---|
| `RECORDED` | The application answered 200 | "Received by the application" |
| `NOT_RUNNING` | Transport error, or gateway 502/504 | "Not delivered: {Preview or Live} was not running" |
| `APPLICATION_UNAVAILABLE` | 503 from the receiver (no source runs, or recording failed) | "Not delivered: the application could not record it" |
| `REFUSED` | Any other non-200 | "Refused by the application (HTTP {status})" |
| `TIMED_OUT` | No answer within `delivery.timeout` | "Not delivered: the application did not answer in time" |
| `TOKEN_UNAVAILABLE` | `dropTokenUnavailable` | "Not delivered: the platform could not sign it" |
| `QUEUE_FULL` | `dropQueueFull` or `dropScopeQueueFull` | "Dropped: too many events at once" |

These are recorded where the relay already calls `drop` or finishes `deliver` (`delivery.go:22-58`,
`slack_events.go:221-226`). Drops before routing (`dropNoRoute`, `dropMalformed`,
`dropInvalidSignature`) have no scope and are not listed per scope. A `dropNoRoute` whose
installation matches ACTIVE TeamSpace connections is counted per TeamSpace connection (count and
last time; review 9), in a map of at most 1024 connections. That count is the Slack workspace's
events that no Project was listening to. It appears on the connection's Setup row as "N events
from this Slack workspace reached no Project since {buffer_started_at}" and is returned by the
same RPC for the scope's selected connections.

The fates `STARTED_RUN` (with FlowID), `DELIVERED_TO_RUN` (with FlowID) and `FILTERED` (with a reason) require a report from the application.

### API and authorization

- `ConnectorService.ListRecentTriggerEvents(project_id, scope)` returns the scope's entries,
  newest first.
- `internal/dexai/api` declares the port `RecentTriggerEvents` that it consumes, and
  `internal/dexai/bootstrap` injects the relay.
- Authorization is the Project's: builder for Preview, operator for Live.
- The panel is the Events view of the Runs column (Runs | Events), for Preview and Live, since
   removed the environment tab's menu (the current contract). It polls every 5 s while open; there is
  no Watch.

### Tests

The tests use no scripted receiver; an `httptest` handler standing in for the application is a
forbidden substitute (review 9). They extend `internal/dexai/agents/slack_relay_integration_test.go`,
whose scenario application is a real application with a real `sdkgo/webhooktrigger` receiver
behind the lane's gateway. They cover:

- `RECORDED` from the real application;
- `NOT_RUNNING` after its Preview is stopped;
- `APPLICATION_UNAVAILABLE` and `REFUSED` only as far as the real receiver produces them. A fate
  the real application cannot be made to produce is listed as unproven scope in the pull
  request, not faked.
- the no-route count;
- the byte truncation;
- the 51st entry evicting the oldest;
- no text in an entry;
- the API authorization, in `internal/dexai/api`.

## Publishing stages

[the current contract](flow-changes.md); the Studio side is
`components/publishing/progress/` ([README](../../../web/studio/components/publishing/progress/README.md)).
Tools ▸ Publishing shows one publish as a bar of five stages in plain words, over the phases that
the release and deployment Flows already record: `ReleaseBuildRevisionSnapshot.phase_timings` and
`DeploymentSnapshot.phase_timings`, labelled by `components/publishing/timings/phase-labels.ts`.
`lib/project/publishing/publish-stages.ts` declares the stages once, each with its phases. The
backend freezes the configuration before it builds (FreezeReleaseBuildConfiguration runs first and
its timing comes first, `internal/dexai/delivery/releases/README.md`), so Lock settings comes before
Build; this corrects the prototype's order.

| Stage | Phases and facts | Cancel |
|---|---|---|
| **Check** | No phase timing. The Publish popover's commands: `DeriveLiveConfiguration` builds the copy, then `StartReleaseBuild` is admitted (replay, the Preview validation or the reason, a stale copy). A refusal returns to the popover, which stays open with the problem. The stage is done once the build revision exists | nothing to cancel yet |
| **Lock settings** | `CONFIGURATION_FREEZE`: the copy is frozen under the build. A freeze refused for gaps fails here, with each gap linked to its Setup row | cancels the build |
| **Build** | The builder's `QUEUE`, `HOST_PREPARATION`, `DEPENDENCY_PREPARATION`, `ANALYSIS`, `COMPILATION` and `IMAGE_PUBLICATION`, then `ARTIFACT_PUBLICATION` | cancels the build, until its automatic deployment is admitted |
| **Deploy** | The automatic deployment (`auto_deploy_status`), then the deployment's `AdmitDeployment`, `AcquireProjectDeployment`, `VerifyReleaseAndConfiguration`, `PublishDefinitions`, `PrepareLiveExecution` with its parallel branches `PrepareLiveTemporal` and `PrepareLiveAccess`, `StartLiveDex` and `ApplyApplication` | cancels the deployment |
| **Go live** | `VerifyReadiness` (the health check), `CommitCurrentLive`, `RequestReplacedRetirement` and `ReleaseProjectDeployment` | cancels the deployment during `VerifyReadiness`; disabled once the health check passed, and the server refuses from `CommitCurrentLive` on |

- **Progress.** A phase's timing is committed when the phase ends, so a stage is done when its
  last phase has a timing, and the running stage is the first one that is not done.
- **The build's phases arrive together.** The builder measures a finished build, so Build's
  phases are committed when it ends; while it runs, the [build log](#build-log) is the live view.
- **Durations.** A stage's duration is its wall-clock span, from its first phase's start to its
  last phase's end; the parallel preparation branches overlap and are not added up.
- **Not in the bar.** `RetireReplacedGeneration` belongs to the replaced generation's own
  retirement ([the current contract](flow-changes.md)).
  `CleanupCancelledDeployment` runs after a cancel, shown as "Cancelling…".
- **Other publishes.** A rollback, `StartDeployment` of an older build, shows only Deploy and Go
  live. Publishing Live's own version again with waiting settings shows all five, and Check
  records `LIVE_CODE`. On a narrow panel only the running, failed or cancelled stage keeps its
  name.
- **Outcome.** The header reads "Publishing V13" with Cancel while it runs, "Published V13 · took
  2m 48s" when done, "V13 didn't go live · Live still runs V12" with the problem panel under the
  bar when a stage failed, and "Publishing cancelled · Live still runs V12" after a cancel.

### Cancel

- Cancel (`progress/CancelPublish`) first confirms briefly that the running Live is untouched.
  Studio sends `ReleaseService.CancelReleaseBuild` while the build can still be cancelled, and
  `DeploymentService.CancelDeployment` once its deployment is admitted. A build cancel refused
  with `RELEASE_BUILD_DEPLOYING` names the deployment in its ErrorInfo metadata `deploymentId`,
  and Studio cancels that instead. Each target keeps one request ID, so a repeated click is the
  same cancellation.
- Studio disables Cancel once `VerifyReadiness` has succeeded, because the Flow commits the new
  Live next. The server decides a race: once the Flow has chosen `CommitCurrentLive`, the cancel is
  refused with `DEPLOYMENT_NOT_CANCELLABLE`, and Studio says "Too late to cancel".
- A FAILED deployment that never went live can be cancelled too; its cleanup then releases it
  instead of leaving it for recovery.
- A cancel during Live preparation lets the running preparation round finish, because the branches
  hand the identities they minted to the Project, and the next Step boundary routes to cleanup. A
  cancel in `StartLiveDex`, `ApplyApplication` or `VerifyReadiness` preempts the running Step; a
  preempted `ApplyApplication` is cleaned up until its last round has ended (1m43s).
- Cancelling never touches the running Live: its generation, route, Worker endpoint, connection
  usages and Slack trigger registrations stay.
- A cancelled build revision ends `CANCELLED` (`RELEASE_STATUS_CANCELLED`), and a cancelled
  deployment ends `CANCELLED` (`DEPLOYMENT_STATUS_CANCELLED`); each snapshot carries its
  `cancellation`, who asked and when. Until the status is reached, Studio shows "Cancelling…". A
  cancelled publish is not a failure, and a new build is admitted afterwards, as after a failure.
- Live history lists it as `PUBLISH_CANCELLED` ([timeline](#environment-timeline)).
- Authorization is the Project's build capability, as for starting the build or deployment.

The Flow side, the RPCs that request cancellation, their safe points and cleanup, is in
[flow changes](flow-changes.md#next-publish-and-cancellation).

## Build log

Hosted build-log reads require hosted verification.

- **The read.** `ReleaseService.GetReleaseBuildLog(project_id, release_id, build_revision_id,
  cursor)` returns `lines`, at most 64 KiB per read, `next_cursor` and `complete`. `complete` is
  true once the build revision has settled and no line remains.
- **The cursor** is opaque to Studio. `Builder.ReadLog(cursor)` reads from it: CloudWatch's
  `nextForwardToken` with the build's attempt for the hosted builder, an absolute byte offset for
  the local one. It replaced `LogTail`: a read with `Tail` reads the log's end, which the Agent's
  build log tail uses.
- **Whole lines.** A read returns whole lines, so a value is never split across two reads where
  redaction could not see it.
- **Redaction.** Every line is redacted on the server before it is returned, as the Agent's build
  log tail is today: configured values of at least 8 bytes from the build revision's frozen
  configuration, longest first, then the Project's infrastructure names. The redactor moved from
  `internal/dexai/agents/diagnostics` to `internal/dexai/projects/config` as `OutputRedaction`, so both reads
  use one.
- **Never stored.** The platform reads the builder's own log on each call; nothing is copied into a
  Flow, the read model or a file.
- **Polling.** `lib/project/publishing/use-build-log.ts` reads while the log is open, at most
  every 2 seconds and no sooner than the read's `retry_after_ms`, and stops when `complete` or when
  the log is hidden. Under load a page is empty at the same cursor with a longer `retry_after_ms`.
- **Deploy and Go live.** Their lines come from the deployment's phases and failure through
  `WatchDeployment`. There is no deployment log store, and the application's own log on a failed
  health check stays an Agent tool for now.
- **Authorization.** The Project's build capability, as Tools requires; a stranger is refused.
- **Cost.** Per replica the viewers of one page share one read and its result for 2 seconds, and
  a token bucket of `builds.logReadsPerSecond` (default 2, burst 4) bounds the reads it starts; a
  throttled provider read backs off per build from 4 seconds to a minute. Nothing is stored
  ([deployments](../../../internal/dexai/delivery/deployments/README.md#build-logs)). The
  `GetLogEvents` quota, its source and date, and the number of viewed builds at which it binds are
  in [flow changes](flow-changes.md#api-additions).

## API changes of

Implemented by  and described in the [resource API](../../api/resource-grpc.md#build-cancellation-and-the-build-log).

| Change | Where |
|---|---|
| `CancelReleaseBuild(project_id, request_id, release_id, build_revision_id)` and `GetReleaseBuildLog(project_id, release_id, build_revision_id, cursor)` returning `lines`, `next_cursor`, `complete`, `retry_after_ms` | ReleaseService (8 to 10 RPCs, after 's removal in ) |
| `CancelDeployment(project_id, deployment_id, request_id)` | DeploymentService (4 to 5 RPCs) |
| `RELEASE_STATUS_CANCELLED` and `DEPLOYMENT_STATUS_CANCELLED` | the status enums |
| `Cancellation{actor_account_id, requested_at}` as `cancellation` on `ReleaseBuildRevisionSnapshot` (17) and `DeploymentSnapshot` (14) | ReleaseService, DeploymentService |
| The ErrorInfo reasons `RELEASE_BUILD_NOT_CANCELLABLE`, `RELEASE_BUILD_DEPLOYING` (metadata `deploymentId`) and `DEPLOYMENT_NOT_CANCELLABLE`; `FAILURE_REMEDY_CANCEL_DEPLOYMENT` | shared |
| `EnvironmentEvent.Kind` `KIND_PUBLISH_CANCELLED` | ProjectService |

Migration `0009_publish_cancellation.sql` adds the nullable `cancellation JSONB` columns that the
read model projects ([database](README.md#database-schema-changes)).
