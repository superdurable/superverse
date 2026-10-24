# Account tables

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Owns SQL transactions for accounts, audience-bound login sessions, bounded one-use authorization codes, revocation, and the platform admin check.

Callers: `internal/dexai/agents/remoteagent`, `internal/dexai/api/tenancy`, `internal/dexai/bootstrap`, `internal/dexai/projects` and `internal/dexai/tenancy/accounts` import this package.

Dependencies: it imports `internal/comm/platform/postgres` and calls the PostgreSQL API.

```mermaid
flowchart LR
  self["internal/dexai/tenancy/accountdao"]
  c0["internal/dexai/agents/remoteagent"] --> self
  c1["internal/dexai/api/tenancy"] --> self
  c2["internal/dexai/bootstrap"] --> self
  c3["internal/dexai/projects"] --> self
  c4["internal/dexai/tenancy/accounts"] --> self
  self --> d0["internal/comm/platform/postgres"]
  self --> e0(["PostgreSQL API"])
```
<!-- END GENERATED PACKAGE SUMMARY -->

The only SQL of the accounts. It holds the queries, the row types and their
transactions, and no business rule: `internal/dexai/tenancy/accounts` hashes
passwords, signs tokens and decides what a result means.

| Method | Query |
| --- | --- |
| `Register` | `INSERT … ON CONFLICT (canonical_email) DO NOTHING`, else the registered row |
| `AccountByEmail`, `IsPlatformAdmin` | One account |
| `CreateLoginSession` | In one transaction: the ACTIVE account at the verified credential revision `FOR SHARE`, deletion of its expired logins, the new login (the same session and token hash again is a success) |
| `AuthenticatedAccount` | The login joined with its account: unexpired, same token hash and authentication epoch, ACTIVE |
| `DeleteLoginSession` | Revocation; an absent login is already revoked |
| `IssueAuthorizationCode` | Locks account and issuer, removes its expired codes, enforces five outstanding codes and admits one 60-second grant |
| `ExchangeAuthorizationCode` | Locks account, issuer and grant, rechecks S256 and audience, then atomically inserts the product session and consumes the grant |

`ErrNotFound` is an absent row or a login that is not admitted; every other
error names its operation.

## Tests

`TestAccountDAOIntegration` runs every query against the lane's PostgreSQL as
the server role, including stale credential revisions, expired logins, a new
authentication epoch and a replaced token hash, and deletes its rows.
