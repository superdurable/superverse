# Bridge state

Purpose: Keeps the Bridge's local state in a 0700 directory of atomically written 0600 files: the single-instance lock, bindings, tokens, progress and unacknowledged reports.

Callers: `cmd/superverse-bridge`, `cmd/superverse-bridge/mcpserver` and `cmd/superverse-bridge/relay`.

Dependencies: `internal/dexai/agents/remoteagent/bridgeapi` and `internal/comm`; it calls no remote API.

The directory is `--state-dir`, else `$SUPERVERSE_BRIDGE_STATE_DIR`, else `superverse-bridge` under
the user's configuration directory (`~/Library/Application Support` on macOS, `~/.config` on Linux).
It is a client cache, not a source of truth: the platform owns the Session, the executor and every
outcome.

```text
<state dir>/                   0700
  serve.lock                   flock held by the one serve process
  bindings.lock                flock held while a binding is added
  bridge.sock                  the local socket between mcp and serve (0600, see mcpserver)
  calls.lock                   flock serializing trace writes and rotation (0600)
  calls.jsonl[.1|.2]           private timing trace, three files of at most 4 MiB each
  bindings/<connectionId>/     0700, at most 8 bindings
  bindings/.staging-*/         a binding being assembled; never listed
    token                      the connection token
    binding.json               BindingRecord: endpoint, Project, Orca session and versions
    progress.json              Progress: epoch, generation and boundary, delivery cursor and the
                               sending record, acknowledged report sequence, last imported Orca
                               message, recent turn generations and delivered Orca message IDs,
                               and the last committed report slot
    reports/<slot>.json        unacknowledged reports in order, at most 1,024 or 8 MiB
```

- Every file is replaced by writing a synced temporary file in the same directory and renaming it,
  so a reader (`status`) or a crash never sees a partial file. Records decode strictly.
- `AddBinding` holds `bindings.lock` (a blocking `flock`) across the check of the bound and of the
  Orca session's uniqueness and the publication, assembles the binding in a `.staging-` directory
  and publishes it with one rename, so a crash leaves no partial binding and concurrent connects
  cannot bind one Orca session twice or pass 8 bindings. It removes staging directories a crash
  left; `Bindings` lists only published bindings.
- `Lock` takes an exclusive, non-blocking `flock` on `serve.lock`; a second `serve` fails with
  `ErrLocked`, and `Running` probes it for `status` and `disconnect`.
- `StoreReports` writes new reports to the slots after the last one, then commits them with the
  caller's Progress in the one atomic write of `progress.json`, which records the last committed
  slot: reports and the progress that accounts for them (such as the event cursor) are durable
  together. A slot file after the committed one is a write a crash cut short; it is never read and
  is overwritten later.
- Reports are stored in local slots, in the order they were made. Only the head report, the one
  being sent, carries its sequence (the acknowledged sequence plus one): a report the Session
  refuses does not advance its sequence, so the next report reuses it, and a restart resends the
  head with the sequence it already had. `StoreReports` refuses with `ErrStoreFull` at 1,024 reports
  or 8 MiB, unless the store is empty, so the reports of one commit always fit eventually; the relay then refuses new MCP calls until acknowledgements free space.
- The Unix socket path must fit the macOS limit of 103 bytes; a longer state directory is refused
  with the advice to choose a shorter one.
