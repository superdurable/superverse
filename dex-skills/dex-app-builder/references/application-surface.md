# Stage 2: establish the confirmed application surface

Read [UI workflow](ui-workflow.md), then use exactly one mode.
Superverse Studio supplies authenticated process management; when no separate
participant UI is requested, select No custom UI and continue without a UI-choice
question. Keep the business contract review proportional to the application.
A request for an admin portal, dashboard, or management backend is not itself a
reason to select Custom UI. Select it only when the completed management UI
capability mapping records a specific interaction Superverse Studio cannot provide.

### No custom UI

Use Superverse Studio as the only process-management UI. Define indexed
Attributes, `GetDexSummary`, `GetDexDisplay`, editable fields, human-action
Steps, and Action RPCs with conditions and permissions so Studio Runs and Inbox
cover the confirmed process.

Adapt the template to retain only its future-ready architecture:

- a non-business React/Vite page in `web/src/App.tsx`;
- the Go HTTP server and its `getHealth` operation, which serves the `dex-app.yaml` health path;
- the OpenAPI source and generation pipeline;
- locally generated Go server interfaces and TypeScript client;
- the template's Makefile targets.

Remove every custom process-management operation and surface: approval, rejection, retry, escalation, status, display, list, search, detail, Action or Attribute proxies, dashboards, forms, queues, and mock state. Remove their handlers, services, fixtures, generated usages, and tests. This includes the template's text-summary Flow, endpoints and form when the process does not summarize text (see `AGENTS.md`). Do not keep speculative endpoints.

When the process needs an inbound webhook, receive it through a Connector Trigger: the provider's dedicated Trigger, or the catalog's generic `webhook` connector for a sender without one. Do not turn webhook ingress into a second management backend.

Do not run the Custom UI wireframe checkpoint in this mode. If the product later needs custom UI behavior, reuse the retained architecture and enter the custom-UI workflow before implementing it.

### Custom UI

Require the confirmed management UI capability mapping to name the unsupported
interaction and keep all covered management operations in Superverse Studio. Valid
gaps include a participant portal, a fundamentally different navigation model,
or a complex interaction the platform does not provide. “The project needs a
backend” is not a capability gap.

If a material interaction choice is unresolved, create a low-fidelity static
wireframe in the template React/Vite frontend to clarify that choice. A clear
request already authorizes its stated page, fields and behavior; implement it
directly without requiring this optional checkpoint.
Represent the necessary pages with headings, labels, placeholder boxes, inputs,
buttons, and ordinary links. Multiple pages must have stable, directly openable
paths and working link navigation.

The wireframe may use React for static markup, but it must not use application
state or effects, API or generated-client calls, local storage, timers, a mock
server, Mock Controls, provider integrations, images, custom icons, animation,
branding, or visual polish. Buttons and inputs remain inert unless a link is
needed to show the approved navigation path. Use minimal neutral styling and do
not model loading, validation, success, failure, or recovery behavior yet.

Check the frontend with `npm --prefix web run build`. After `commit_and_push`,
the user opens the pages in Studio Preview; give the user a compact inventory of
page names, purposes, and paths.
Ask only about the unresolved page, field, action placement or navigation choice.
Do not turn this checkpoint into an aesthetic review or iterate on
visual design.
