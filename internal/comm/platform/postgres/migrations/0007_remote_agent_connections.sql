-- Connection credentials are server-owned; executor authority stays in the
-- SessionAgentFlow Executor Attribute. Projects is a Dex-owned read model,
-- so project_id deliberately has no foreign key.
CREATE TABLE remote_agent_connections (
  connection_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  account_id TEXT NOT NULL REFERENCES accounts (account_id),
  login_session_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('PAIRING', 'PAIRED', 'REVOKED')),
  pairing_code_hash TEXT UNIQUE CHECK (pairing_code_hash ~ '^[0-9a-f]{64}$'),
  pairing_expires_at TIMESTAMPTZ NOT NULL,
  token_hash TEXT UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
  basis_epoch BIGINT NOT NULL CHECK (basis_epoch >= 0),
  binding JSONB,
  last_seen_at TIMESTAMPTZ,
  revoke_reason TEXT CHECK (revoke_reason IN ('platform', 'disconnected', 'removed')),
  created_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  CHECK (
    (status = 'PAIRING' AND pairing_code_hash IS NOT NULL AND token_hash IS NULL AND revoked_at IS NULL AND revoke_reason IS NULL)
    OR (status = 'PAIRED' AND pairing_code_hash IS NULL AND token_hash IS NOT NULL AND revoked_at IS NULL AND revoke_reason IS NULL)
    OR (status = 'REVOKED' AND pairing_code_hash IS NULL AND token_hash IS NULL AND revoked_at IS NOT NULL AND revoke_reason IS NOT NULL)
  )
);
CREATE INDEX remote_agent_connections_project ON remote_agent_connections (project_id, status);
GRANT SELECT, INSERT, UPDATE, DELETE ON remote_agent_connections TO superverse_server;
