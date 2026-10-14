-- ProjectReleaseFlow ReleaseBuildRevision.FailureOwner: who must act on the build's failure or
-- its auto-deploy failure. Null for builds without a failure and for builds synced before this
-- column. It holds no secret.
ALTER TABLE release_build_revisions ADD COLUMN failure_owner JSONB;
