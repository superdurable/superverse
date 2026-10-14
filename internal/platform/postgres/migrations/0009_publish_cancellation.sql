-- Publish cancellation (decision 0027): a person's request to cancel a build or a deployment, who
-- asked, when and in which phase, projected from ProjectReleaseFlow's ReleaseBuildRevision.Cancellation
-- and ProjectDeployFlow's Cancellation Attribute. Only the Dex Attribute Store writes them; null when
-- nobody cancelled and for rows synced before these columns, with no backfill. They hold no secret.
ALTER TABLE release_build_revisions ADD COLUMN cancellation JSONB;
ALTER TABLE deployments ADD COLUMN cancellation JSONB;
