# Application template

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
