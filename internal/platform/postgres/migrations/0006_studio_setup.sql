-- Studio setup (docs/architecture/studio-setup/README.md, "Database Schema Changes"). Two
-- columns on read-model projections that their Flows already own; neither holds a secret.

-- ProjectReleaseFlow ReleaseBuildRevision.PreviewValidation: how the build met the publishing
-- rule. Null for builds created before this column.
ALTER TABLE release_build_revisions ADD COLUMN preview_validation JSONB;

-- ProjectDeployFlow StartIdentity: the actor, request ID and fingerprint of the start. Null for
-- deployments that started before this column.
ALTER TABLE deployments ADD COLUMN start_identity JSONB;
