-- TeamSpace connections (docs/architecture/teamspace/connections.md). The
-- server owns these tables through internal/tenancy/connectiondao, so they
-- carry constraints. They hold no secret: credentials live only in the
-- platform bucket's objects, which the rows reference. No foreign key points
-- at the synced team_spaces or projects tables, whose rows may appear later.
CREATE TABLE team_space_connections (
  team_space_id TEXT NOT NULL,
  connection_id TEXT NOT NULL,
  creation_fingerprint TEXT NOT NULL,
  name TEXT NOT NULL,
  connector_id TEXT NOT NULL,
  module_path TEXT NOT NULL,
  version_line TEXT NOT NULL,
  manifest_version TEXT NOT NULL,
  auth_method_id TEXT NOT NULL,
  oauth_client TEXT NOT NULL CHECK (oauth_client IN ('user', 'platform')),
  settings JSONB NOT NULL,
  revision BIGINT NOT NULL,
  credential_ref JSONB,
  credential_revision BIGINT NOT NULL DEFAULT 0,
  credential_status TEXT NOT NULL CHECK (credential_status IN ('AUTHORIZING', 'READY', 'REAUTHORIZATION_REQUIRED')),
  credential_issued_at TIMESTAMPTZ,
  credential_expires_at TIMESTAMPTZ,
  refresh_lease_owner TEXT,
  refresh_lease_until TIMESTAMPTZ,
  connection_status TEXT NOT NULL CHECK (connection_status IN ('ACTIVE', 'DELETED')),
  creator_account_id TEXT NOT NULL REFERENCES accounts (account_id),
  updated_by_account_id TEXT NOT NULL REFERENCES accounts (account_id),
  last_failure JSONB,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (team_space_id, connection_id));
-- A name is unique within a TeamSpace, connector and major version; deleted
-- connections keep their rows and free their names.
CREATE UNIQUE INDEX team_space_connections_name
  ON team_space_connections (team_space_id, connector_id, version_line, name)
  WHERE connection_status <> 'DELETED';
CREATE INDEX team_space_connections_recent ON team_space_connections (team_space_id, created_at DESC, connection_id DESC);
-- A connection ID is unique on its own, so a configuration entry's
-- sharedConnectionId names one connection; a selection checks its TeamSpace.
CREATE UNIQUE INDEX team_space_connections_id ON team_space_connections (connection_id);

-- A Project scope uses a connection: it authorizes the scope's credential
-- reads and protects the connection from deletion.
CREATE TABLE team_space_connection_usages (
  team_space_id TEXT NOT NULL,
  connection_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('preview', 'live')),
  added_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (team_space_id, connection_id, project_id, scope),
  FOREIGN KEY (team_space_id, connection_id) REFERENCES team_space_connections);
CREATE INDEX team_space_connection_usages_scope ON team_space_connection_usages (project_id, scope);

-- One random token per Project scope authorizes its credential reads. Only
-- its SHA-256 is stored; token_secret_ref pins the scope's secret object that
-- holds the token, which configuration documents reference.
CREATE TABLE scope_credential_tokens (
  project_id TEXT NOT NULL,
  scope TEXT NOT NULL CHECK (scope IN ('preview', 'live')),
  token_hash TEXT NOT NULL UNIQUE,
  token_secret_ref JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (project_id, scope));

GRANT SELECT, INSERT, UPDATE, DELETE ON team_space_connections, team_space_connection_usages, scope_credential_tokens TO superverse_server;
