# Stage 4: integrate the Custom UI, verify, polish, and hand off

Read [build, test, and handoff](build-test-handoff.md) and [API tests](api-tests.md). Run narrow checks while iterating (`make check-fdg-v2`, a build, `make test-app RUN=<affected tests>`); `commit_and_push` then runs the whole gate: `make check-static`, the platform's Flow-definition check and `make test-app` with its coverage and requirement trace. Run all requested real acceptance scenarios. Every Flow must pass FDG 2.0 JSON analysis with `valid: true`. A source gate does not prove business execution; a missing test service must not be reported as a pass.

Every application ships API tests in `internal/apptest` that cover each
confirmed requirement with the connectors' mock packages ([API tests](api-tests.md));
there are no browser test suites, fixtures or other test-framework dependencies.
Mocked tests verify source only. Source checks and both production builds
remain required. A missing private Connector key leaves
configured execution pending; it does not block handoff of complete, checked
source to Studio Configuration.

For No custom UI, verify regeneration and compilation, the production build,
absence of management routes, the strict FDG Summary, Display and Actions that
Studio renders, and any retained webhook Trigger. Do not keep mock routes, mock state
or mock E2E in the product as dormant scaffolding.

For Custom UI, implement the Go backend and generated TypeScript client calls
against the same OpenAPI contract. Do not wait for private provider credentials
before completing source. Implement the confirmed loading, validation, empty, success,
failure, retry, recovery, and terminal behavior. The configured real journey
runs in Studio Preview after the user completes Configuration. Connector mocks
appear only in `internal/apptest`; do not introduce component mocks or intercepted
API responses elsewhere. Report a case requiring unavailable dependencies as
incomplete, separately from source readiness.
Do not create an application-level mock server. Do not create a second Go business backend, product mock routes,
or user-visible Mock Controls.
Mock evidence never replaces real Dex durability, Connector, or application E2E evidence.

Only after the real Dex and Connector end-to-end journey passes in Studio Preview may the Custom
UI add images, custom icons, branding, animation, refined responsive behavior,
or other visual polish. Recheck the affected real journey and production build after polishing; this does not require adding an application test suite. Do not generate or source visual assets
before this stage.

Use a real Dex Server for waits, RPCs, Channels, retries, Worker replacement, terminal behavior, Inbox permission history, and connector boundaries. Use deadline-based convergence rather than fixed sleeps.

### Project release and hosted deployment handoff

Before handoff, keep `dex-app.yaml` complete. `make check-fdg-v2`, part of the
`make check-static` gate, renders every declared Flow as strict FDG 2.0 and
checks that each connector version matches `go.mod`; `commit_and_push` also
validates `dex-app.yaml`. Treat missing, duplicate, or diagnostic-bearing Flow
Definitions as release failures. Superverse renders the definitions again when
it builds a Release.

Live Publishing is project-scoped. Select an eligible main-branch commit,
prepare its exact FDG/manifest, configure that source, build the whole application
with its frozen configuration reference, and select a successful Release to deploy.
Preview uses the current clean, pushed Sandbox commit in Build Configuration and
Preview controls, without choosing or creating a Live Release. Do
not filter commits by author or by whether an agent created them. A selected
Flow Type changes only Build or Runs inspection; Publishing has no Flow Type
deployment selector and deploys every Flow Definition in the Release.

For hosted Connector configuration, require validation against the selected
source's exact manifest/FDG and freeze the accepted revision, object version and
digest. The platform supplies trusted `DEX_PROJECT_*` references and scoped AWS
credentials; the official SDK loads and verifies the snapshot before Workers or
application goroutines start. Ordinary settings need a new build/deployment or
Preview start. Shared environment credentials take effect on the next connection
use; the SDK alone performs supported expired-credential refresh. Missing or
mismatched configuration, snapshot digest, capability or workload identity blocks
deployment. This platform management boundary does not require a new Dex
release.

Finish with:

- confirmed business, UI-mode, permission-boundary, and connector decisions;
- implemented and verified behavior;
- selected connector releases and any reported capability gap;
- exact test evidence;
- remaining limitations and release blockers;
- a clean default-branch Git commit suitable for project-level Publishing;
- the Studio Configuration (connections and credentials) still needed before
  Preview or deployment.

Publishing, deployment and Preview are the user's actions in Studio: Publishing
prepares the selected Live commit, configures it, builds and deploys the accepted
Release; Build Configuration and Preview start a Preview. When the user requests
either, point to those Studio steps rather than claiming to perform them. Report
only observed Release, configuration revision, deployment, and E2E identities;
never fabricate a deployment result.
