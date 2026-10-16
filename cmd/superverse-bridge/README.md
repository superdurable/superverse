# superverse-bridge

Purpose: The program a Builder runs next to Orca to connect a Codex conversation to a Superverse Project as its remote agent.

Callers: the Builder, the Orca plugin, and Codex (which starts `mcp`).

Dependencies: `cmd/superverse-bridge/mcpserver`, `orca`, `platform`, `relay` and `state`, and `internal/dexai/agents/remoteagent/bridgeapi`; it calls the remote agent endpoint and the Orca runtime.

The [remote agent protocol](../../docs/api/remote-agent.md) defines the Bridge connection. `make build-bridge-local`
builds it for this machine into `bin/superverse-bridge`, with `BRIDGE_VERSION` (default
`git describe`) as the version it reports when pairing.

| Command | What it does |
|---|---|
| `connect --endpoint <origin> [--session <id> \| --new --worktree <selector>] [--worktree <selector>] [--codex-config <path>]` | Without `--session` or `--new`, lists Orca's Codex conversations on the terminal with their title, worktree directory, last activity and session ID, and reads the choice: a structured conversation no binding holds; one already bound or running in a terminal is shown as unavailable, and "New conversation" comes last only with `--worktree` (`id:<worktreeId>` or `path:<dir>`). `--session` binds that session directly and `--new --worktree` creates a conversation without asking. It checks that choice and the Codex configuration, then asks for the pairing code on the terminal (never an argument, never logged), so an error in either never spends the code, binds the Orca session (stopping its idle Codex process so that the brief starts it again, on the same thread, with the superverse tools, and showing the chat in Orca) and runs `serve` in the foreground, or hands the binding to a running `serve`. |
| `serve` | Holds the state directory's lock, listens on the local socket and runs every binding's relay. |
| `mcp` | The MCP server Codex starts; serves the tools of `$ORCA_AGENT_SESSION_ID`. |
| `status` | One line per binding, from the running `serve`, else from the state directory. |
| `trace` | Reads the bounded local call trace and prints each call's timing segments plus p50, p90 and p99. |
| `disconnect [--connection <id>]` | Revokes a binding's connection and removes it. |
| `version` | The version, OS and architecture. |

If the Bridge restarts while Codex is working, the running Codex turn is stopped; tool calls it had
not yet sent are reported as interrupted; calls already sent (or possibly sent) may still run and
finish, their results arrive as messages, and the Bridge accepts new work only after they settle.
Send a new message to continue. If the Orca conversation was cleared or rebound, the binding stops
with "the Orca conversation changed; reconnect from Studio".

The Bridge carries no agent instructions of its own: everything Codex is told (the platform agent's
rules, the repository context, where the Dex skills are, how messages name their sender) comes from
the platform's `sync` answer (`agentContext`), and the Bridge only relays it into its first message
and the MCP server's `instructions`, with the platform's tool descriptions. Its own text is
transport framing and status.

Every command takes `--state-dir` ([state](state/README.md)). Logs go to standard error and never
contain message text, tool arguments or tokens.

`trace --state-dir <dir>` reads `calls.jsonl` and its two rotations from that directory. Each
JSON line holds a local call ID, remote call ID when assigned, tool name, five timing points and
an outcome code. The three files are capped at 4 MiB each. No arguments, results or messages enter
the trace; a call interrupted before its remote report has missing intermediate points.
