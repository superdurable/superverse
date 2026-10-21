# Source handoff and configured acceptance

Application source is the coding Agent's handoff boundary. Every Flow ships with
API tests in `internal/apptest` that drive it through the HTTP API with the
template harness (`apptest.Start`) and the connectors' mock packages; add no
browser test suites, fixtures or other test frameworks. Connector mocks never
appear outside `internal/apptest`. Mocked tests verify source, never provider
behavior. Preserve unrelated tests in an imported existing application unless
their removal is authorized.

A new template project starts with the reference `TextSummaryFlow`. Copy its
structure for the requested Flows. Unless the request needs text summarization, remove it with
its worker registration, `summary-model` connection, text-summary endpoints and
form, rather than keeping it alongside the business Flow. Preserve unrelated
business Flows in imported or already-developed applications; this rule applies
to the starter reference, not existing user work.

Prepare locked dependencies once in a fresh checkout before generation or
checks. The platform starts the bootstrap command the template declares
(normally `make bootstrap`: `npm --prefix web ci` and the declared Go modules)
when it checks the workspace out; run it yourself once before the first
generation or check, which waits for the platform's run and then restores only
what changed. Do not repeatedly run checks with missing generator executables
or reinstall unchanged dependencies after every edit. This restores the
committed lockfiles; it is not a dependency upgrade.

The commit_and_push tool runs the committed source gate: the checkStatic target
(generation, strict FDG, formatting/modules/vet, type checking and both
production builds), the platform's own Flow-definition check, which Preview also
applies, and the template's testApplication target (`make test-app`) on a fresh
test engine, which then requires that every Flow ran, every wired branch of a
mocked connector was returned, the engine stayed healthy and every requirement
of the current version is traced to passing tests. Use focused checks while editing
(`make check-fdg-v2`, a build, `make test-app RUN=<affected tests>`); do not
duplicate the complete gate immediately before calling the tool. Generate
ignored client packages before resolving their imports. Never start your own Dex
or Temporal: `make test-app` provides the test engine.

When implementation and focused checks are complete, call commit_and_push with
blocked=false and let its automatic source gate perform final verification. Missing private Connector
credentials or a not-yet-configured Preview/Live target are later
configuration/acceptance work, not incomplete source; a connector without mocks
is listed as untested by the gate and does not block it. Say exactly which real execution remains unverified; do not claim a
successful provider call or deployment. If a required source change or source
check remains incomplete, use blocked=true and preserve the concrete blocker.
An explicit request for executed integration testing still remains incomplete
until the actual configured dependency path passes; never invent that evidence.

In Studio, configure credentials through Studio after pushed source is
prepared. The host validates actual Preview and Live runs separately. Never
embed keys in source or replace the Connector with a fake provider to make a
check pass. Keep repository documentation and code comments in English.
