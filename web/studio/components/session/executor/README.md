# Session executor controls

<!-- BEGIN GENERATED DIRECTORY SUMMARY (scripts/check-readmes.mjs; edit only Purpose) -->
Purpose: Shows the composer's Agent picker, SQL-derived remote liveness, offline notice, pairing entry and epoch-fenced return to Superverse.

Importers: `components/session` and `components/session/start` import this directory.

Imports: it imports `components/session/remoteagent`, `components/ui`, `lib/api`, `lib/browser`, `lib/session`, `lib/session/credentials`, `lib/session/model` and `lib/shared`.

```mermaid
flowchart LR
  self["components/session/executor"]
  c0["components/session"] --> self
  c1["components/session/start"] --> self
  self --> d0["components/session/remoteagent"]
  self --> d1["components/ui"]
  self --> d2["lib/api"]
  self --> d3["lib/browser"]
  self --> d4["lib/session"]
  self --> d5["lib/session/credentials"]
  self --> d6["lib/session/model"]
  self --> d7["lib/shared"]
```
<!-- END GENERATED DIRECTORY SUMMARY -->

The canonical Session resource supplies executor authority and SQL last-seen metadata. Ninety seconds without a last-seen update displays offline; that label never switches execution. The UI clock refreshes liveness and Send availability and makes no request.
The existing Session Watch includes SQL last-seen updates; no extra poll is added.
Offline keeps the draft editable, disables new sends and retains already queued
messages for reconnect. It changes no server admission or executor authority.

Operators can read the executor label and remote liveness. Only Builders can connect an agent or switch execution; the server checks that capability independently of the menu.

Studio sends the exact protobuf epoch string with `SetSessionExecutor`, then refreshes through the existing Session owner and its stale-response guards. Project, permission, Session status or executor epoch/generation changes unmount the command owner and abort its request. Refusal or response loss also refreshes canonical state without retrying the command. The server authorizes every command and rejects stale epochs.

The composer holds one Agent picker for both platform and remote modes. Its
first level chooses Superverse or a local agent. The Superverse level lists
models only while platform execution is current; remote mode offers the
epoch-fenced switch back. The local level shows liveness and opens the
Project-keyed RAPAIR dialog. Escape and outside press close the picker;
its nested levels retain keyboard focus. The remote composer accepts ordinary
local agent input and hides the mock composer. Shared Agent conversation UI receives
only the existing public message projection and labels remote assistant replies.

An absent executor on a present agent description renders the legacy
Superverse selection and permission-gated Connect action only. It creates no
executor state or epoch and never offers switching. Mock Studio fixtures do
not implement executor switching; their authorized command returns 503.

Model choices stay disabled after a successful update until the canonical Watch
or an explicit Session refresh displays its next model revision. A synchronous
component guard also rejects a second click before React renders. The existing
command owner admits the one request and reports failures; the picker never
replays a failed mutation. Executor switching retains its existing epoch fence.

When Platform is current and the canonical Executor has a suspended identity,
the picker offers Use the local agent with that exact ID, owner/host and SQL
liveness. It sends the observed decimal epoch and connectionId through the
existing single-flight command, refreshes canonical state on completion or
refusal, and never treats another saved PAIRED row as an automatic target. No
new polling or browser authority is introduced. Without a suspended target the
existing pairing dialog is unchanged.
