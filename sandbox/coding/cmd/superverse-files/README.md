# superverse-files

`superverse-files` serves the Agent's file tools inside one coding Sandbox:
`read`, `edit`, `write`, `grep` and `glob`. sandboxd's file API reads bytes only,
cannot search and cannot leave `/workspace`; this helper reads by line, edits
idempotently, searches with ripgrep and applies the path rules of
`sandbox/coding/internal/confinement`. The
[tool orchestration contract](../../../../docs/architecture/agent-harness-design/tool-orchestration.md#files-and-confinement)
owns the tools; this README owns the exact command line and JSON.

```sh
superverse-files read|edit|write|grep|glob --root AGENT_ROOT [--id CALL_ID] < request.json
```

`--root` is the trusted agent root: `/workspace` for the main agent, a
subagent's worktree for a subagent. `edit` requires `--id`, the identity of the
call. The request is one JSON object on stdin with exactly the fields below;
an unknown field is `invalid_arguments`.

## Answers and exit codes

Every operation prints one JSON object on stdout.

- Exit code 0: `{"result": TEXT, "path": PATH, ...}` on success, or
  `{"failure": {"code", "owner", "effect", "message"}}` for a call the model
  corrects. A failure changed nothing (`effect` `none`); its owner is `call`,
  or `platform` for `platform_command_unavailable`.
- Exit code 1: `{"error": "..."}`, a usage or helper error that says nothing
  about the files.

`path` names the confined path: relative to the agent root inside it, absolute
in a read-only root.

## Path rules

A path is absolute or relative to the agent root. It resolves through its
symbolic links (a path that does not exist through its nearest existing
ancestor) and must land in the agent root, which may be read and written, or
in a read-only root: `/opt/superverse/dex-skills`, `$GOMODCACHE`,
`/usr/local/go/src`, `/var/lib/superverse/preview/logs` or
`/tmp/superverse-exec/runs`. A path outside both, a write into a read-only
root, and any path with a `.git` component relative to its root are refused
with `path_outside_roots`. The rule is for isolation and correctness, not
security: the helper and the Agent's commands share one UID.

**Permissions.** The files of the roots are the agent's own: its commands share
the helper's UID and may change their permissions, as `chmod 000` does. A path
that a directory without search permission hides resolves through its nearest
ancestor that resolves, so the path rules above still decide first. When the
permissions of a confined path, or of a directory on its way, then deny the
operation, it answers `path_permission_denied`, a failure of the call that
changed nothing. A permission error on the helper's own state outside the roots,
such as the edit intent records, stays a helper error.

## Operations

| Operation | Request | Result | Failures |
| --- | --- | --- | --- |
| `read` | `path`; `offset` (first line, 1-based, default 1); `limit` (lines, 1 to 2,000, default 2,000) | `cat -n` lines (number, tab, text); a line over 2,000 characters is cut and marked; at most 64 KiB; when lines remain, a last line names the next offset, also in `nextOffset` | `path_not_found`, `path_is_directory`, `binary_file` (a NUL in the first 8 KiB or a line that is not UTF-8), `path_outside_roots`, `path_permission_denied` |
| `write` | `path`, `content` (at most 4 MiB) | `Created <path> (N lines, B bytes)` or `Overwrote <path> (...)`; missing directories are created; the same content again changes nothing | `path_is_directory`, `path_outside_roots`, `path_permission_denied` (the file is unreadable or its directory unwritable) |
| `edit` | `path`, `old_string`, `new_string`; `replace_all` (default false) | `Edited <path>: N replacement(s), lines a-b` | `edit_no_match`, `edit_ambiguous` (more than one occurrence without `replace_all`), `edit_noop` (equal strings), `binary_file`, `file_too_large` (over 8 MiB), `path_not_found`, `path_is_directory`, `path_outside_roots`, `path_permission_denied` |
| `grep` | `pattern` (a regular expression); `path` (default the agent root); `glob`; `output_mode` (`files_with_matches` default, `content`, `count`); `context` (0 to 20 lines, content mode); `head_limit`, `offset` (result lines); `case_insensitive` | Matching paths, `path:line:text` lines with context, or `path:count` lines, sorted by path; at most 32 KiB, with a line that says how to read on | `invalid_pattern`, `path_not_found`, `path_outside_roots`, `path_permission_denied` (the path is a file it cannot read or a directory it cannot list) |
| `glob` | `pattern` (a ripgrep glob such as `**/*.go`); `path` (default the agent root) | Matching file paths, most recently modified first; at most 200 paths and 16 KiB | `invalid_pattern`, `path_not_found`, `path_outside_roots`, `path_permission_denied` (the path is a directory it cannot list) |

Missing directories are created with mode 0750. Writes replace a file atomically (a temporary file beside it, renamed over it)
and keep its permissions.

**Edit intent records.** Before it replaces the file, `edit` records the file's
SHA-256 before and after and its result in
`/tmp/superverse-files/edits/<id>.json`. A repeat of the same `--id` on the same
file returns the recorded result when the file holds the edited content, and
applies the edit once more only when the file is still exactly as before. The
records are references for retries, never a gate; they are removed after two
hours.

**Search.** `grep` and `glob` run
`rg --no-config --hidden --color never ... --glob '!.git'` with the pattern after
`-e` and the path after `--`, so neither can be read as a flag. The `.git`
exclusion comes last and takes precedence over a caller's glob; `.gitignore` is
honored. A preflight `rg ... -e PATTERN -- /dev/null` reports a pattern or glob
that does not parse as `invalid_pattern`. A missing `rg` is the platform fault
`platform_command_unavailable`. ripgrep skips what it cannot read below the
search path, such as a file whose permissions deny reading it, and exits with
code 2: the result keeps what it found (or `No matches found`, `No files found`)
and ends with a note, within the budget, that names the first three skipped
paths from ripgrep's messages:
`[not searched, because ripgrep could not read them:\nPATH: Permission denied (os error 13)]`.

## Verification

`TestFileHelperActualSandbox` in `internal/agents/sandbox/workspace/sandboxd`
runs every operation through the real sandboxd of a coding image container,
including the path rules, the permission failures and the repeat of an edit.
