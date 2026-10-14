-- The Slack event relay (docs/architecture/teamspace/connections.md, "Slack
-- event relay"). A Slack connection's authorization stores the workspace it
-- was installed in, which routes the platform Slack application's events; an
-- application's start registers the connection names whose Slack triggers
-- receive them. Neither holds a secret.
ALTER TABLE team_space_connections
  ADD COLUMN provider_account_id TEXT,
  ADD COLUMN provider_organization_id TEXT;
CREATE INDEX team_space_connections_provider_account
  ON team_space_connections (connector_id, provider_account_id)
  WHERE provider_account_id IS NOT NULL AND connection_status = 'ACTIVE';

-- A registration belongs to its usage row and goes with it.
CREATE TABLE slack_trigger_registrations (
  team_space_id TEXT NOT NULL,
  connection_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  scope TEXT NOT NULL,
  connection_name TEXT NOT NULL CHECK (length(connection_name) BETWEEN 1 AND 256),
  registered_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (team_space_id, connection_id, project_id, scope, connection_name),
  FOREIGN KEY (team_space_id, connection_id, project_id, scope)
    REFERENCES team_space_connection_usages ON DELETE CASCADE);

GRANT SELECT, INSERT, UPDATE, DELETE ON slack_trigger_registrations TO superverse_server;
