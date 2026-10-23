# Application template

## Live Worker capacity

The template uses Dex Go SDK 1.15.0. Live supplies `maxConcurrentTasks: 4`; the Worker leaves `ReservedRPCTasks` unset and inherits the SDK default of one RPC slot. Steps are declined once total busy handlers reach three, while RPCs may use all four slots. The runtime YAML has no reserve field. This SDK-side admission works with Project Dex Server 1.13.0; the Live SDK floor remains 1.13.0. Preview retains push mode.

## Local verification model transport

The shared configuration loader exposes `LocalModelHTTPClient` for example LLM connections.
When `SUPERVERSE_LOCAL_MOCK_LLM_URL` is absent, it supplies no client override and the
released connector opens exactly as before. Only the platform's Local Native and
Kind profiles enable `connections.mockLLM` and inject this reserved variable into
Preview and Live; AppManifest and Studio cannot set `SUPERVERSE_` variables.

When present, the URL must equal the loaded platform credential broker's origin
and `/internal/mock-llm/v1/responses`, with local storage enabled. The transport
accepts only OpenAI Responses requests with the fixed non-secret credential
`superverse-local-mock-llm-not-a-secret`. It never forwards real credentials,
follows redirects, uses a proxy or falls back to a provider. The mock HTTP service
lives in the platform, outside the application; connector mocks remain confined
to `internal/apptest`. Local verification uses `superverse-mock-summary` and checks
the deterministic summary and token counts through real application Flows.

Hosted profiles never enable this seam. An explicitly approved real-LLM local run
uses `connections.mockLLM: false`; the ordinary connector then handles its actual
provider credential.

## Optional connections

A `dex-app.yaml` connector entry may set `"optional": true`; omitted or false
means required. Preview and Live can run without a selection for an optional
connection. Selected connections still need valid settings and authorization.
The application must handle typed absence before calling its connector and show
"Connect <name> to use this feature" instead of failing startup. Text-summary
and its inherited summary-approval example keep their pages available without
summary-model; generation records a connect-an-LLM notice with no provider call.
Newsletter and Slack approval connections remain required because their external
triggers and delivery are essential. The released SDK's ordinary eager opener
is unchanged; use the template's typed availability pattern for optional features.

Text-summary persists `connection_not_configured` as a typed tag in its existing
Step result before routing to RecordSummaryFailure. That Step uses the recorded
cause, not the replacement Worker's current configuration, so applying settings
or restarting a Worker cannot rewrite an already-decided failure notice.
