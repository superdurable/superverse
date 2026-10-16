-- Accounts and login sessions are database-authoritative
-- (docs/architecture/teamspace/accounts.md). The roles exist before any
-- migration runs; each environment's private procedure creates them.
CREATE TABLE accounts (
  account_id TEXT PRIMARY KEY,
  canonical_email TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL CHECK (status = 'ACTIVE'),
  password_record JSONB NOT NULL,
  credential_revision BIGINT NOT NULL,
  auth_epoch BIGINT NOT NULL,
  registration_fingerprint TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL);

CREATE TABLE login_sessions (
  session_id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts (account_id),
  token_hash TEXT NOT NULL,
  auth_epoch BIGINT NOT NULL,
  issued_at TIMESTAMPTZ NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL);
CREATE INDEX login_sessions_account ON login_sessions (account_id);
CREATE INDEX login_sessions_expiry ON login_sessions (expires_at);

GRANT SELECT, INSERT, UPDATE, DELETE ON accounts, login_sessions TO superverse_server;
