# Application surface workflow

Use this only after discovery completes the management UI capability mapping.
Design Summary RPC, Display RPC, Action RPC, Indexed Attribute, Inbox, and
permission coverage before selecting a mode. Needing an admin or management
interface does not by itself justify Custom UI; that mode requires a recorded
Superverse Studio capability gap.

## No custom UI

Superverse Studio owns process management: Runs, Inbox, Start Flow, Display and
Actions. With no separate participant UI requested, select this mode without a
redundant question. The template retains:

- a non-business React page in `web/src/App.tsx`;
- the Go HTTP server and OpenAPI source;
- locally generated Go server interfaces and TypeScript client;
- the `getHealth` operation, which serves the `dex-app.yaml` health path;
- the template's Makefile targets.

Remove process state, management operations, Action/Attribute proxy endpoints,
dashboards, forms, mock state, fixtures, and related tests, including the
template's text-summary endpoints and form when the process does not summarize
text. The shell must not present approval, display, status, list, search,
detail, retry, or escalation controls.

Retain a webhook only when it is confirmed integration ingress. Receive it
through a Connector Trigger: the provider's dedicated Trigger, or the catalog's
generic `webhook` connector for a sender without one, including an internal
system.

Do not require wireframe approval for the inert shell. Confirm that no
management route remains; the `make check-static` gate that `commit_and_push`
runs verifies generation and both production builds. If custom behavior is
requested later, follow the workflow below before connecting it to production.

## Custom UI

Enter this mode only for the unsupported interaction named in the confirmed
management UI capability mapping. Keep operations already covered by Superverse
Studio on its Runs, Inbox, Display, Action, edit, timeline, and graph surfaces.

### 1. Resolve material interaction choices

A clear implementation request already authorizes its stated page, fields and
behavior. Implement those directly. Use the optional static checkpoint below
only when a consequential interaction choice remains unresolved; ask about that
choice, without requiring another approval of already supplied requirements.

Agree only on the information architecture needed to unblock backend design:

- role-specific entry pages and ordinary link navigation;
- necessary list, detail, create, action, and completion pages;
- labels, inputs, buttons, and placeholder regions on each page;
- which Studio surfaces remain available to maintainers.

Implement those pages as static markup in the template React/Vite frontend.
React remains the rendering shell, but this checkpoint has no hooks or
application state, API imports, generated-client calls, local storage, timers,
mock lifecycle, Mock Controls, or provider behavior. Use no images, generated
art, custom icons, animation, branding, gradients, or decorative effects. Keep
CSS neutral and minimal. Inputs and buttons are inert; use ordinary links or
link-styled buttons only to demonstrate navigation between directly openable
page URLs.

Check the pages with `npm --prefix web run build`. After `commit_and_push`, the
user opens them in Studio Preview; give the user a page inventory with each
name, purpose, and path. Ask about the unresolved page set, navigation, fields or actions.
Do not ask the user to approve colors, typography, imagery,
responsive refinements, dynamic states, or production behavior at this point.

### 2. Contract and backend design

Once interaction requirements are known, design the Flow, Connector
capabilities, and application OpenAPI contract together. Map every approved UI
action to an application operation backed by a Flow start, typed RPC, query, or
confirmed ingress. Define authentication and permission enforcement,
idempotency, asynchronous status, validation, response and error shapes, retry,
and terminal outcomes.

OpenAPI describes the application's business boundary. It must not expose Dex
Steps, Channels, Attributes, connection credentials, or other runtime internals.
Update `openapi/openapi.yaml` and run `make generate` before implementing HTTP
handlers. The Go backend implements the generated server interfaces, and the
browser later consumes the generated TypeScript client. Do not hand-write
parallel transport types.
The generated directories are ignored local build artifacts. Regenerate them in the workspace, but never edit or commit them.

Implement and verify the Go Flows and required Connectors before returning to
dynamic frontend work. A missing public Connector capability is a blocker to
report to the user.

### 3. Integration and durable verification

Implement the Go boundary and UI against the same generated OpenAPI contract.
Replace any wireframe's inert controls with generated TypeScript client calls.
Missing private credentials defer real execution, not source implementation.
Add the confirmed loading, validation, empty, success, failure, retry, recovery,
and terminal behavior. The browser calls the application API; it never imports
a Dex client or accesses raw Dex primitives.

Follow the template's test rule: no test suites or mocks unless the user asks,
and requested tests use real APIs. Loading, validation, failure, retry and
terminal states are exercised through real APIs in Studio Preview; record each
case that was not executed as a coverage gap. Do not add component mocks, intercepted API
responses, fake providers, an application-level mock API or Mock Controls.
Prove waits, RPCs, retries, Worker replacement, provider effects and terminal
behavior through the actual Dex and Connector end-to-end path.

Do not claim application-level UI controls provide platform RBAC. A permission
selector filters work; it does not grant permission. Enforce identity-to-
permission mapping at the trusted application boundary. Keep credentials and
provider secrets server-side.

### 4. Visual polish

Only after the real end-to-end journey passes may the UI add imagery, custom
icons, branding, animation, refined typography, decorative styling, and
responsive fine-tuning. Preserve the generated client boundary and confirmed
behavior. Then recheck the real end-to-end journey in Studio Preview,
accessibility, and the production build.
