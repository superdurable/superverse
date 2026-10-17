---
name: dex-app-builder
description: Primary entry point for designing and building Dex applications and process products on the Superverse go-react-v1 application template. Use for Dex product, application, or workflow requests. Orchestrates business discovery, the UI decision, Go backend implementation, connector integration, source verification, and Superverse Studio handoff.
---

# Dex App Builder

Build the smallest coherent Dex product that satisfies the user's request. A
clear implementation request authorizes its stated UI and routine engineering
choices. Ask only for missing business facts, access or consequential choices;
do not turn discovery, design, wireframes or source checks into repeated gates.
The requirements card (`confirm_requirements`) shows what you build before code for a
new application or a large change, one that alters the current requirements. It does
not wait: the Session records it as the next version and you keep building, and the
user changes it with a message, which you answer with a revised card. A revised card
keeps every recorded feature's number and never reuses a removed one, and a completely
different request gets a card that replaces the requirements. Make a small change, one
that leaves them as they are, directly. Only a card recorded before the first build of a
Project that started from the default template carries the design of the Flows it adds,
removes or changes; every later card, and every card of a Project that started from an
example, carries none, because Studio draws the real Flows
([Confirmation artifact](references/product-discovery.md#confirmation-artifact)).

## Session start and routing

- Product work stays here; read [Dex SDK](../dex-sdk/SKILL.md) when implementing
  its backend. A missing released connector capability is an explicit gap, never
  something to rebuild from low-level wire types or a direct provider call: the
  Session's main agent chooses connectors with a `connector_select` subagent,
  puts the connections on the requirements card (asking first when several
  could serve a need), and contributes each gap upstream with a
  `connector_develop` subagent. [Dex Connector Contributor](../dex-connector-contributor/SKILL.md)
  is that subagent's guide.

## Source authority and efficient discovery

Inspect the existing manifest and source before selecting tools. The
repository's `AGENTS.md` lists the exact files to change and its reference Flow;
start there. Dex Server is backward compatible with every earlier SDK release,
so the application may move its Dex Go SDK, Connector SDK and connectors to newer
stable releases when useful, after reading the release's "Breaking Changes"
section; the `go.mod`-selected module source and the compiler are the API
authority. Read [workspace bootstrap](references/workspace-bootstrap.md) only
for dependency changes or bootstrap failures. Remove the reference Flow when the
requested process does not need it; retain the reusable server/API skeleton.

Read only references relevant to the next implementation decision, using their
headings to select a bounded span. Retain verified version/API facts and do not
reread whole guides after every tool call. Batch independent read-only requests
when the host supports it; discover filenames before reading module source.
Implement once the required types and lifecycle are understood. Let concrete
compiler, strict FDG or provider diagnostics drive further investigation.

For external products, search the official connector catalog with
`connector_catalog`. Select a released operation and kind, run the returned
`install_command` (`go get <module>@<version>`), then inspect that exact
module's `connector.yaml`, generated operation factory and input/output types
with the file tools in the directory that `go list -m -f '{{.Dir}}' <module path>` prints. The catalog identifies
capabilities and current releases; it is not provider business data or a
historical release feed. Do not guess APIs or substitute low-level Connector SDK
wire types for the released operation factory. Read the relevant
[Connector architecture](references/connector-architecture.md) section for
external integration, text generation, Trigger ingress or hosted configuration.

## Essential product constraints

- Go backend only; strict FDG 2.0 only. The application template is the stack authority.
- Superverse Studio owns Runs, Inbox, Start Flow, Display, Actions, edits,
  history and diagnostics. Do not recreate management APIs or UI. A separate
  participant interaction is a valid custom UI; a backend requirement alone is
  not.
- Use typed Flow Attributes and exact-loaded AttributeMaps as the default
  authoritative store. Justify an external store with a concrete access/scale
  gap and one owner per fact. No speculative database, projection or cache.
- Use parallel Steps for one lifecycle, independent top-level Flows for separate
  owners, and no SubFlows unless the Core gate and explicit confirmation apply.
  Never split when the parts would exchange RPCs or messages on frequent events
  (every operation, tool call or poll): each call between Flows is recorded in
  both histories and costs an engine operation, even when it only reads.
- Put provider effects in Execute, with identity, bounded timeout and recovery.
  Run a long poll that would run beside other ongoing work in its own SubFlow
  ([Polling](../dex-sdk/references/core/patterns.md#a-long-poll-beside-other-work)).
  WaitFor declares durable conditions and performs no provider or Dex mutation.
- Stable FlowID, complete RequestID, explicit reuse policy and typed error
  recovery govern starts. An accepted start returns known identity without a
  preflight or success-path state read. Streams are best-effort observations.
- Define each Flow's identity, lifecycle, Steps, typed RPCs, all primitives,
  selective loads, locks/CAS, retention, retries and uncertain-effect handling
  in the application before coding. Preserve open execution compatibility.
- Keep credentials outside code, Flow state, logs and definitions; the user sets
  them in Studio Configuration. Keep the template's `internal/connectorconfiguration`
  loader: Preview and Live supply the trusted `DEX_PROJECT_*` scope and
  key/version/digest that official `projectconfig.LoadFromEnvironment` verifies.
  No credential broker or mounted config workaround.
- Browser permission selection grants no authority. Enforce actor permissions
  at the trusted server boundary; use stable granular Action permissions.
- Do not mutate another repository. Publishing, deployment and starting Preview
  are the user's actions in Studio.

## Read the matching implementation reference

| Decision | Reference and scope |
| --- | --- |
| Missing business requirements, actors or ownership | [Business contract](references/business-contract.md); record concise decisions instead of requesting already supplied facts |
| Participant/custom UI or management coverage | [Application surface](references/application-surface.md) and [UI workflow](references/ui-workflow.md); clear requests need no extra static-wireframe approval |
| Go Flow rendering, Summary/Display, Start input or Inbox Actions | [FDG authoring](references/fdg-authoring.md): exact syntax and focused diagnostics |
| Flow, persistence, RPC, Connector or OpenAPI changes | [Backend implementation](references/backend-implementation.md), selecting the relevant headings; use the Dex SDK Core/Go routing rather than loading all references |
| Verification, source handoff or deployment | [Verification and handoff](references/verification-handoff.md); distinguish source readiness from configured real execution |
| API tests, connector mocks, a failed test stage | [API tests](references/api-tests.md) |

Keep the design proportional and in the application's owning documentation.
Test every Flow through its API in `internal/apptest` with the connectors'
mock packages ([API tests](references/api-tests.md)); add no other test
framework. Existing/requested real acceptance remains required. Generate
ignored API packages before resolving their imports. `commit_and_push` runs the
template's complete gate (`make check-static`, the Flow-definition check, then
`make test-app`);
use focused checks while editing, then fix its actual diagnostics rather than
running the same full gate immediately beforehand. Never report compilation as
provider/E2E success.
