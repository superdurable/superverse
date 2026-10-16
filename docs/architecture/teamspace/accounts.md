# Accounts

Accounts and login sessions are database-authoritative. They have no lifecycle that
needs durable execution, so there is no AccountFlow: registration, login, logout and
authentication are single transactions in PostgreSQL.

## Tables

```sql
CREATE TABLE accounts (
  account_id TEXT PRIMARY KEY, canonical_email TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL CHECK (status = 'ACTIVE'), password_record JSONB NOT NULL,
  credential_revision BIGINT NOT NULL, auth_epoch BIGINT NOT NULL,
  registration_fingerprint TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
CREATE TABLE login_sessions (
  session_id TEXT PRIMARY KEY, account_id TEXT NOT NULL REFERENCES accounts (account_id),
  token_hash TEXT NOT NULL, auth_epoch BIGINT NOT NULL,
  issued_at TIMESTAMPTZ NOT NULL, expires_at TIMESTAMPTZ NOT NULL);
CREATE INDEX login_sessions_account ON login_sessions (account_id);
CREATE INDEX login_sessions_expiry ON login_sessions (expires_at);
```

- `password_record` is the Argon2id record; a login token is stored only as its
  SHA-256. Token and cookie formats do not change.
- The tables are written only by the account code, through `internal/dexai/tenancy/accountdao`.

## Behavior

- **Registration** is idempotent: replaying the same request with the same password
  logs in; any other registration of the email is `ALREADY_EXISTS`.
- **Authentication** of every request is one query, `login_sessions` joined with
  `accounts`, that accepts only an unexpired session whose epoch matches the account's.
  It returns `Caller{AccountID, CanonicalEmail, PlatformAdmin}`; the platform admin is the
  configured ACTIVE platform administrator account (`accounts.platformAdminEmail`).
- **Logout** deletes the session; raising `auth_epoch` ends every session of the account.
- **Expired sessions** are never accepted. Login deletes the account's expired sessions;
  there is no background cleanup.
- The process-local authentication cache and its interceptor are removed: a query per
  request is cheaper than keeping a cache consistent with logout.
- The three `IsAdministrator` reads (Project authorization, the mock model check of the
  Agent API and the Project API) read `Caller.PlatformAdmin`.

## Tests

Real PostgreSQL integration tests cover idempotent registration, login and logout,
rejection of expired, revoked and stale-epoch sessions, the platform admin rule, and
migrations that rerun cleanly and refuse a changed checksum.
