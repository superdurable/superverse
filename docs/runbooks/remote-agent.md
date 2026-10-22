# Remote agent connection operation

The [connection API](../api/remote-agent.md) implements pairing, safe connection
listing/revocation, credential authentication/disconnect and environment downloads.
Pairing does not run Codex; the executor layer implements activate/sync/report
and epoch-fenced switching in the same Session. Never claim an active connection from a PAIRED database row.

## Local connection addresses

Native's default `remoteAgent.listen` is `127.0.0.1:26089`. A named instance gets
the normal instance offset; read its effective `native-run.yaml` or run record
rather than assuming the default. Its Studio Connect dialog shows the exact
`http://127.0.0.1:<port>` origin. The launcher owns port admission
and fences; do not start, stop or change another instance to free a port.

Kind's ordinary `sv2 kind ports` forwards `127.0.0.1:28089` to the server Service's
8088 port. Its dialog shows `http://127.0.0.1:28089`. There is no
Kind Ingress controller to install for this endpoint. Hosted environments serve
`https://<Studio-host>` through the existing server Ingress and
ALB, with the ordinary server health check; no new LB, listener or certificate.
Use the existing deployment procedure, never an ad hoc cloud change.

After the platform has been rebuilt/deployed from reviewed source, open the Agent picker beside the composer,
choose My local agent, then Connect on a READY Project with Builder capability. Read and acknowledge the
local-machine warning. Copy the one-time ten-minute code into either the Orca
Bridge's interactive `dexai-connect connect` flow or the direct macOS command
`dexai connect --endpoint <Studio origin>`. The direct command then asks which
agent to use; Codex is available and Claude Code is shown as not available yet.
Use the dialog's displayed origin as `--endpoint`. The Bridge appends the exact
`/remote-agent/v1/...` route paths; do not append that prefix to the endpoint. Neither code
nor token belongs in command arguments, shell history, screenshots, traces,
logs, receipts or source. The Bridge keeps its returned token in its owning
private configuration; Studio receives no token.

For a new Project, select My local agent on the start form. After the Project
is READY and its Session ACTIVE, acknowledge the inline warning and run the
displayed `dexai-connect connect --endpoint <origin>` command, entering
the code interactively. The page renews an expired code while retaining the
first message. It sends that message when the Session Watch reports remote
authority. If the send result is uncertain, inspect the Project conversation
before sending the original text manually; do not assume a retry is safe.

Refresh connections explicitly to observe a PAIRED credential. Remove an unused
connection through Studio; disconnect revokes only the Bridge's authenticated
credential. Switch to the Superverse executor before removing the current
connection. That switch revokes only its replaced credential: subsequent requests
with that token return 401. A still-valid credential replaced by another remote
activation returns 409 `executor_changed`; reread authority rather than retrying
a stale command. The menu retains the exact observed epoch and reconciles the
Session after switching. Offline never switches automatically; messages remain
queued for reconnect and ordinary answer/steer/Stop actions stay available. An unknown/expired/used code requires a new
code, and a revoked/expired login requires a new authenticated pairing. A Project
role loss returns 403; a deleting/deleted/missing Project returns 404; unavailable
dependencies return 503. A full pairing gate returns 429 with `Retry-After`;
back off rather than opening waiting requests. Keep those observations distinct.
Unexpected errors retain private matched-route/method, controlled failure/type
and PostgreSQL SQLSTATE/constraint diagnostics in the server log. Raw error text,
value-bearing database fields, headers and request bodies are never logged;
the public error body remains generic.

## Environment downloads

The default distribution root is `/opt/superverse/bridge`, configurable by
`remoteAgent.downloadsDir`. Publish only the five fixed filenames and one
`manifest.json` using `bridgeapi.DownloadsManifest`, with at most five artifacts
and a 64 KiB manifest. Build/distribution is owned by the Bridge layer. Minimum
version is configured by the server, default `0.1.0`. Local Native/Kind explicitly
allow the exact `0.0.0-dev` version; hosted environments reject that exemption.

A missing local distribution yields an empty public manifest and 404 for a
missing artifact. A malformed manifest or escaping symlink is 503, not successful
coverage. Artifact paths cannot expose arbitrary files. Diagnose distribution
content privately and verify SHA-256; never place credentials under this root.

The local command is `dexai-connect`. Its default state directory and environment
variable are `dexai-connect` under the user's configuration directory and
`DEXAI_CONNECT_STATE_DIR`. Existing pre-launch state is not migrated automatically;
use `--state-dir` with the old directory if a binding must be kept, or pair again.
The fixed server download artifact names remain `superverse-bridge-*` for the
existing wire contract.

Orca must be at least 1.4.0 and Codex at least 0.162.0; 1.4.225 and 0.163.0
are the newest verified versions. A newer local version connects with a
compatibility warning at startup and in `dexai-connect status`.

The direct `dexai` command has a separate default state directory (`dexai` under
the user configuration directory, or `DEXAI_STATE_DIR`). Build its local macOS
binary with `make build-dexai-local`. It offers the agent before the pairing
code and marks Codex/Claude Code installed or missing using the user's login
shell PATH. Missing agents offer the documented vendor install command with
explicit `Y/n` confirmation; declining prints the command and exits. Logged-out
agents offer interactive login, then recheck it. These actions default to No
without a TTY, even with piped confirmation. The CLI README records the vendor
commands, official sources and verification date. Only after setup, supported
versions, login and the Codex daemon check does it ask for the Studio code.
Claude Code must be at least 2.1.41 for its documented `auth status` and `auth login` commands. It receives only the platform MCP tools; its built-in tools are disabled.
The Builder's installed Codex CLI and
shared daemon must both be at least 0.162.0; 0.163.0 is the newest verified
version and newer versions connect with a compatibility warning. `dexai`
starts the daemon with `codex app-server daemon start` when it is absent. If
startup fails, its error includes the Codex message; try that command directly. It
never updates, downgrades or restarts an existing daemon. Installation of a missing
CLI requires the separate confirmation above. The first brief is submitted
before the Codex TUI opens. Each `dexai connect` owns a fresh private connection
directory, so run it again in another terminal to connect another Project or
endpoint; no separate `--state-dir` is needed. Codex opens a separate thread/TUI,
and Claude Code runs a separate headless child. Exit the TUI or press Ctrl+C to
stop and revoke only that connection; the others keep relaying. The Project stays
in remote mode, offline: reconnect with a new pairing code or switch to the
platform agent in Studio. `dexai status` lists every endpoint, Project and
connection ID; `dexai disconnect --connection <id>` stops a selected connection.
If a domain moved, `dexai disconnect` forgets a binding after a definite uncoded
404, missing DNS name or TLS hostname mismatch. It states that platform access
was not revoked; switch to the platform agent in Studio, or pair again to take
over. Transient-looking errors ask before local removal and default to retaining
the binding without a TTY. `dexai disconnect --local` explicitly skips revocation.
A definite missing API holds the relay with a clear status problem instead of
retrying forever. No token is printed.

Normal exit deletes its private directory after revocation; failed cleanup keeps
an offline record for disconnect to finish. At most eight connections share a
state root, including retained records. `dexai serve` resumes only legacy flat
bindings; new foreground connections do not resume after exit.
For Claude Code on macOS, dexai checks installation and sign-in first and offers
setup when needed (version 2.1.41 or newer). It runs headless while Studio remains
the conversation surface. The pairing dialog and
creation card link to `/help/local-agent` for prerequisites, installation,
provider selection, recovery and local permission guidance.
The Project source remains in the Sandbox. The default local Codex directory
is a private scratch directory; `--cwd` selects an existing local directory.
If Studio reports `delivery_uncertain`, check the thread's public history before
asking the Builder to send a new message; dexai never repeats an uncertain
write automatically.

After the public mirror is published, the CLI release is built locally for four
platforms, with checked SHA-256 digests and a Homebrew formula proposed to
`superdurable/homebrew-tap`. No platform deployment or GitHub Actions job
publishes this CLI.

## Verification and cleanup

Run required-lanes on the frozen integrated source: Native and Kind are both
required because their port/configuration tooling and the chart paths changed.
Compile/static checks do not replace real PostgreSQL, Dex, Sandbox, Git, storage,
BFF/HTTP and the three journeys. Verification defaults to the mock model; any real model or real LLM API requires a
strong, stated reason approved by the platform developer before the run and recorded with the model
in the release record or PR Tests section; RAPAIR connection tests themselves issue no model call. Preserve
exact source/run/image/command outcomes and failures in private records until
behavior validation permits a receipt. Missing fixtures/services fail coverage.

The disposable Studio specs require `SUPERVERSE_E2E_REMOTE_AGENT_FIXTURE`, a
private regular 0600 JSON file with owner `email`, `password` and `projectId`.
Its READY Project must have an idle ACTIVE mock-model Session using the platform
executor, no connections, queued/steered messages, pending input or tasks.
For the multi-person cases, include `builder: {email, password}` for a distinct
registered ordinary TeamSpace MEMBER with no Project role. For navigation,
include `otherProjectId`, a distinct READY owner Project in the same TeamSpace.
Run the cases serially on this exclusive fixture with no other active Bridge.

The [remote executor specs](../../web/studio/tests/e2e/integration/remoteagent/README.md)
cover pairing/removal, public replies, a second Builder's takeover, actual
last-seen expiry/reconnect and ordinary queued delivery, plus browser
cancellation during navigation or real permission demotion. A narrowly matched
browser gate delays only one switch before dispatch; it never fabricates the
backend's response or holds Watches. A separate actual BFF request proves the
403 after demotion. Traces, screenshots and videos are disabled.

Cleanup preserves the first failure and reconciles uncertain effects without
retrying a mutation. It removes only owned queued IDs, then stops/switches an
executor only if its exact connection still owns authority, revokes its own
connections, removes its added membership and closes its own logins. An
unresolved cleanup remains a failure requiring exact-identity reconciliation.
DAO tests use real lane PostgreSQL scratch databases; gRPC/HTTP cases use the
actual handlers and Project/Dex/resources. Source and static checks alone prove
no runtime acceptance; no token belongs in recordings or evidence.

Project deletion removes its credentials in existing Session cleanup passes,
including absent/failed Sessions, after the SQL projection shows deletion. Never
write projections, bypass auth, globally prune, edit owner/fence files or delete
another test's identities to recover a failure. Capture exact owned identities
and ordinary deletion diagnostics before asking for a source/runtime fix.
