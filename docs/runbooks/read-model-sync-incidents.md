# Read-model sync incidents

This runbook applies to the [read model](../architecture/teamspace/read-model.md).
The Dex Attribute Store writes the Flows' facts into PostgreSQL, and the Flows stay
their only owners. There is no reconciliation loop, resync command or refresh RPC, so
a table that disagrees with its Flow is an incident or a bug, found and fixed by hand.

## When it applies

- **PostgreSQL could not be written for longer than the retry budget**
  (`syncRetryPolicy.totalDuration`, 168 hours). Batches may have been dropped,
  including a member's removal, which would leave that member's access in place.
- **A column name or type does not match its Attribute** and Dex skipped the value. This
  is a bug that the column-set test should have caught; fix the bug as well as the row.
- A report that a list or an authorization disagrees with Dex Web or with the Flow's
  state.
- Flow RPCs across many Projects are slower by about 7 seconds. Dex runs each sync batch
  within the Flow's execution first (superdurable/dex), so a slow PostgreSQL or a lock on
  a synced table holds every syncing Flow. Check PostgreSQL first: `pg_stat_activity` for
  sessions waiting on a lock of a synced table, and the database's latency. Release the
  lock or restore the database; the held batches then complete on their own.

Do nothing for the sync delay itself: a write shows in its table within seconds, and
Studio shows the writer the owning Flow's returned state meanwhile.

## After a long PostgreSQL outage

Do this before service resumes, so that no stale membership is served:

1. Restore PostgreSQL by the environment's procedure. The platform Dex does not start
   while its Attribute Store is unreachable
   ([infrastructure](../architecture/teamspace/infrastructure.md#postgresql)).
2. Capture the Dex address, the pinned dexcli version (`make install-dexcli`) and the
   time the outage began and ended. List the Flows whose rows are in doubt, starting
   with the member tables:

   ```sh
   dexcli flow search --server <platform Dex> --no-hydrate --all \
     --query "FlowType = 'TeamSpaceFlow' AND keyword3 != 'DELETED'"
   dexcli flow search --server <platform Dex> --no-hydrate --all \
     --query "FlowType = 'ProjectFlow' AND keyword3 != 'DELETED'"
   ```

3. For each Flow, read its real state with the pinned dexcli. The `Members` map holds
   account IDs and roles only; add `--no-hydrate` for any other payload.

   ```sh
   dexcli flow state teamspace-<teamSpaceID> --server <platform Dex> --timeout 30s
   dexcli flow state project-<projectID> --server <platform Dex> --timeout 30s
   ```

4. Read the same rows with a read-only connection (the `superverse_server` role) and
   compare them with the map instances:

   ```sql
   SELECT account_id, role FROM team_space_members WHERE team_space_id = '<teamSpaceID>' ORDER BY account_id;
   SELECT account_id, role FROM project_members WHERE project_id = '<projectID>' ORDER BY account_id;
   ```

   Compare any other synced table in doubt the same way: the Flow's Attribute is the
   truth, and `updated_at` and `revision` show which side is older.
5. Correct each differing row to the Flow's value, delete rows of removed members, and
   record every correction. Never change the Flow to match a row.
6. Repeat the comparison; it must show no difference. Then resume service.

**Role for corrections:** an operator corrects synced rows as `superverse_owner`, the
migration role, as a break-glass action, and records it in the incident record (who, when,
which rows, the Flow state it matched). `superverse_dex`'s credentials belong only to Dex's
configuration, and `superverse_server` stays read-only on synced tables; neither is used.

## A column mismatch

Find the Attribute and column from the Flow's definition and the table, fix the name or
type, and ship the fix; a later migration that adds a synced column backfills old rows
itself if they need a value. Correct the affected rows as above. Report the Flow type,
the FlowIDs, the columns and the dexcli, SDK and Server versions.

## Reporting

Report the commands, the Flow identities, the rows compared and corrected, and any
read that failed. A failing `flow state` or `flow history` read is an unresolved
observation gap, not a successful comparison. Keep Flow mutations out of this
procedure; stop, time travel and other mutations need their own authorization.
