-- The read model of the Project, Release and Deployment Flows
-- (docs/architecture/teamspace/read-model.md). The platform Dex Attribute
-- Store writes these tables and nothing else does: the primary key comes from
-- the store's row key template (plus the instance key for a child table), and
-- every other column is the snake_case of a synced Attribute or map field.
-- No foreign keys, no CHECK constraints and NOT NULL only on primary key
-- columns, because a violated constraint fails the whole sync batch.
CREATE TABLE projects (
  project_id TEXT PRIMARY KEY,
  creator_account_id TEXT,
  name TEXT,
  project_status TEXT,
  revision BIGINT,
  template_revision TEXT,
  setup_flow_id TEXT,
  repository_binding JSONB,
  app_manifest JSONB,
  active_deployment JSONB,
  current_live JSONB,
  active_command JSONB,
  last_failure JSONB,
  session_start_failure JSONB,
  project_storage JSONB,
  live_execution JSONB,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ,
  -- Written by ProjectSetupFlow, whose store's row key is setup-{project_id}-{*}.
  setup_status TEXT,
  setup_phase TEXT,
  setup_source_commit TEXT,
  setup_failure JSONB,
  setup_revision BIGINT);

CREATE TABLE releases (
  project_id TEXT,
  release_id TEXT,
  release_status TEXT,
  revision BIGINT,
  source_commit TEXT,
  build_profile_version TEXT,
  prepared_source JSONB,
  active_build_revision_id TEXT,
  ready_build_revision_id TEXT,
  failure JSONB,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ,
  PRIMARY KEY (project_id, release_id));
CREATE INDEX releases_recent ON releases (project_id, created_at DESC);

-- The child table of ProjectReleaseFlow.ReleaseBuildRevisions, one row per
-- build; fields of an instance without a column are skipped.
CREATE TABLE release_build_revisions (
  project_id TEXT,
  release_id TEXT,
  build_revision_id TEXT,
  actor_account_id TEXT,
  configuration_revision BIGINT,
  configuration JSONB,
  build_profile_version TEXT,
  status TEXT,
  revision BIGINT,
  image_reference TEXT,
  image_digest TEXT,
  provenance JSONB,
  artifacts JSONB,
  failure JSONB,
  provider_settled BOOLEAN,
  auto_deploy BOOLEAN,
  auto_deployment_id TEXT,
  auto_deploy_status TEXT,
  auto_deploy_failure JSONB,
  timings JSONB,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ,
  PRIMARY KEY (project_id, release_id, build_revision_id));

CREATE TABLE deployments (
  project_id TEXT,
  deployment_id TEXT,
  release_id TEXT,
  build_revision_id TEXT,
  deployment_status TEXT,
  configuration_revision BIGINT,
  project_deployment_lease JSONB,
  deployment_url TEXT,
  pinned_inputs JSONB,
  workload_grant JSONB,
  readiness_result JSONB,
  deployment_resources JSONB,
  provider_operation_refs JSONB,
  phase_timings JSONB,
  failure JSONB,
  revision BIGINT,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ,
  PRIMARY KEY (project_id, deployment_id));
CREATE INDEX deployments_recent ON deployments (project_id, created_at DESC);

-- Dex upserts parent rows and replaces or deletes child rows; the server
-- only reads them.
GRANT SELECT, INSERT, UPDATE ON projects, releases, deployments TO superverse_dex;
GRANT SELECT, INSERT, UPDATE, DELETE ON release_build_revisions TO superverse_dex;
GRANT SELECT ON projects, releases, release_build_revisions, deployments TO superverse_server;
