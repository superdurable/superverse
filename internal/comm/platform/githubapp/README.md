# GitHub App authentication

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Authenticates as one installation of the GitHub App by signing its JWT and minting installation and repository tokens, and calls the GitHub REST API with them.

Callers: `internal/comm/platform/connectorlibrary`, `internal/comm/platform/githubissues`, `internal/comm/platform/repohost/github` and `internal/dexai/bootstrap` import this package.

Dependencies: it imports `internal/comm` and `internal/comm/platform` and calls the GitHub REST API.

```mermaid
flowchart LR
  self["internal/comm/platform/githubapp"]
  c0["internal/comm/platform/connectorlibrary"] --> self
  c1["internal/comm/platform/githubissues"] --> self
  c2["internal/comm/platform/repohost/github"] --> self
  c3["internal/dexai/bootstrap"] --> self
  self --> d0["internal/comm"]
  self --> d1["internal/comm/platform"]
  self --> e0(["GitHub REST API"])
```
<!-- END GENERATED PACKAGE SUMMARY -->

The GitHub repository host ([`repohost/github`](../repohost/github/README.md)) acts through
this package, and so does every other feature that acts through the same App, such as
platform issue reporting ([`githubissues`](../githubissues/README.md)) and connector pull
requests.

- `App` holds the App ID, the installation and the private key. It caches the
  installation-wide token and renews it a minute before it expires. `Permissions`, when
  set, restricts that token to the permissions a feature needs: the issue installation's
  token holds Issues write and Metadata read and nothing else of the App.
- `RepositoryToken` mints a token restricted to the contents of one repository.
- `Call` sends one JSON request as the installation; `CallAsApp` sends one with the
  App JWT, for the endpoints of the App itself.
- A failed call is an `*HTTPError`, which exposes its status through `HTTPStatusCode`
  so `platform` can classify it. A lost or unreadable response is an
  `*platform.OutcomeUnknownError` that matches `ErrOutcomeUnknown`: the call may have
  taken effect, so the caller reads before it repeats a write.

Every call is bounded to 30 seconds, never follows a redirect, which would carry the
bearer token to another host, and never puts a token in an error.

| File | Responsibility |
| --- | --- |
| `app.go` | The App, its JWT and its installation and repository tokens |
| `request.go` | The bounded GitHub API call and its errors |
| `integration_test.go` | The real installation and App description |
