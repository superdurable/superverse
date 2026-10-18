# Remote agent endpoint client

Purpose: Calls the remote agent endpoint as one Bridge connection: pair, activate, sync, report, disconnect and the downloads manifest, with strict decoding, body limits and backoff.

Callers: `cmd/dexai-connect` and `cmd/dexai-connect/relay`.

Dependencies: `internal/dexai/agents/remoteagent/bridgeapi` and `internal/comm`; it calls the remote agent endpoint over HTTPS.

- The endpoint is an origin; it must be HTTPS unless it is a loopback address (the Native lane).
- Every request body is checked against its `bridgeapi` limit (and a report against
  `Report.Validate`) before it is sent; a request that fails is `ErrInvalidRequest`, never sent. Answers are read up
  to a per-path limit (a sync answer up to 16 MiB, since one large entry may exceed the 1 MiB soft
  budget, which only logs a warning) and decoded strictly: unknown fields and trailing data are
  refused.
- A refusal is a `*RefusalError` with the coded `bridgeapi.ErrorBody`; `RefusalCode` reads its code.
  Refusals are final for their request (400, 401, 403, 409, 413), except the transient codes
  `rate_limited` and `reports_pending` (429) and `unavailable` (503).
- Network failures, gateway answers without a coded body (502, 503, 504) and transient refusals are
  retried with backoff from 1 s to 60 s until the caller's context ends; a 429's `Retry-After` in
  seconds (at most 5 minutes) is the least wait before its retry.
- Redirects are never followed, so the token and body never reach another URL: any 3xx is a
  protocol error, since the endpoint does not redirect. A retried report is the same
  body with the same sequence: after a network failure the Session answers `duplicate` if it had
  admitted it, and after `reports_pending` it admits it once the Session consumed enough reports.
- Each attempt is bounded: 30 s, and for `sync` the 25 s long poll plus 15 s.
- The token is sent only in the `Authorization` header and never logged.
