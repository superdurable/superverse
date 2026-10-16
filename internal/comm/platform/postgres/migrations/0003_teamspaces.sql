-- The read model of TeamSpaceFlow and of the members of TeamSpaces and Projects
-- (docs/architecture/teamspace/teamspaces-and-roles.md). The rules of
-- 0002_read_model apply: only the platform Dex Attribute Store writes these
-- tables, the primary key comes from the store's row key template (plus the
-- instance key for a child table), and there are no foreign keys, no CHECK
-- constraints and NOT NULL only on primary key columns.
CREATE TABLE team_spaces (
  team_space_id TEXT PRIMARY KEY,
  name TEXT,
  team_space_status TEXT,
  creator_account_id TEXT,
  revision BIGINT,
  last_failure JSONB,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ);

-- Written by ProjectFlow's store; a Project never changes its TeamSpace.
ALTER TABLE projects ADD COLUMN team_space_id TEXT;
CREATE INDEX projects_team_space ON projects (team_space_id, project_status, created_at DESC);

-- The child table of TeamSpaceFlow.Members, one row per member. Rows stay
-- when the TeamSpace is deleted; queries exclude DELETED TeamSpaces.
CREATE TABLE team_space_members (
  team_space_id TEXT,
  account_id TEXT,
  role TEXT,
  added_at TIMESTAMPTZ,
  added_by_account_id TEXT,
  PRIMARY KEY (team_space_id, account_id));
CREATE INDEX team_space_members_account ON team_space_members (account_id);

-- The child table of ProjectFlow.Members, one row per member. Rows stay when
-- the Project is deleted, so its members can read the final DELETED snapshot;
-- queries exclude DELETED Projects.
CREATE TABLE project_members (
  project_id TEXT,
  account_id TEXT,
  role TEXT,
  added_at TIMESTAMPTZ,
  added_by_account_id TEXT,
  PRIMARY KEY (project_id, account_id));
CREATE INDEX project_members_account ON project_members (account_id);

-- Dex upserts parent rows and replaces or deletes child rows; the server
-- only reads them. The grants of 0002 on projects cover its new column.
GRANT SELECT, INSERT, UPDATE ON team_spaces TO superverse_dex;
GRANT SELECT, INSERT, UPDATE, DELETE ON team_space_members, project_members TO superverse_dex;
GRANT SELECT ON team_spaces, team_space_members, project_members TO superverse_server;
