# Bridge MCP server

Purpose: Serves the Superverse tools to Codex as a stdio MCP server and forwards listings and calls to serve over the local socket, with zero tools for an unbound session.

Callers: `cmd/dexai-connect` (the `mcp` command, and `serve`, `status` and `disconnect` through the local socket).

Dependencies: `internal/dexai/agents/remoteagent/bridgeapi` and `internal/comm`; it calls no remote API, only serve's local socket.

## MCP

`dexai-connect mcp` is the `[mcp_servers.superverse]` entry of the Codex configuration. It
holds no credentials. Codex passes it `ORCA_AGENT_SESSION_ID` (the entry lists it in `env_vars`);
the session decides the tool set:

- MCP over stdio, newline-delimited JSON-RPC 2.0: `initialize` (revisions 2025-06-18, 2025-03-26
  and 2024-11-05; the tools capability with `listChanged`, and as `instructions` the platform's
  agent context of the bound session, which serve read with `sync` `context: true`, or none when
  serve has not read it), `ping`, `tools/list` and `tools/call`.
  Notifications from Codex are ignored: a cancelled call stays reported to the platform, which
  records its result.
- `tools/list` lists the tool set the platform sent serve at activation. An empty or unbound
  session, or an unreachable serve, lists zero tools, so every other Codex conversation on the
  machine is unaffected.
- After the first `notifications/initialized`, one watch on serve sends
  `notifications/tools/list_changed` whenever the tool set's digest changes, including when serve
  first answers; a repeated notification starts no second watch. Codex 0.162.1 ignores that
  notification and lists tools once when it starts the server, so a Codex process started while
  the session was unbound, serve was down or the relay was not yet activated keeps zero tools until
  Codex restarts; connect restarts it (`Orca.Reload`), and an Orca restart or an idle Codex respawn
  with serve down is not covered.
- `tools/call` arguments must be a JSON object of at most 256 KiB; anything else is a tool error
  at once and never reaches serve.
- `tools/call` runs concurrently, at most 8 calls at once; a call beyond that is answered at once
  with a tool error, while `initialize`, `ping` and `tools/list` are always answered. It returns one
  text content block. A failure that is not the
  tool's (serve unreachable, store full, call interrupted) is a tool result with `isError`.
- A call's response is acknowledged to serve: after the JSON-RPC response's write to standard
  output returned, `mcp` writes one byte on the call's local connection before closing it. A failed
  write, a closed connection or no byte within 30 s means the response did not reach Codex, and
  serve then tells Codex the result as a late-result message instead (relay README, Reading).
- `mcp` ends when standard input ends or on a termination signal, which closes standard input and
  cancels the calls still running. One writer goroutine owns standard output and every message
  waits for its write with cancellation, so a Codex that stops reading standard output cannot keep
  `mcp` from ending: a write blocked in the pipe stays blocked until the process exits, and its
  response counts as not delivered.
- Logs go to standard error; standard output carries only MCP messages.

## The local socket protocol

serve listens on `bridge.sock` in the state directory (mode 0600 inside the 0700 directory; a stale
socket is removed only by the process holding the state lock). Each connection carries one request
line, a JSON object, and is answered by one JSON line; closing the connection cancels the request.
A `call` connection stays open after the answer until `mcp` acknowledges it with one byte (any
byte; a newline) once the MCP response reached its standard output, or closes it without one;
serve waits at most 30 s. Only that byte proves delivery: neither serve's successful socket
write nor a hand-off inside serve does.
A request line is at most 2 MiB and an answer line at most 32 MiB (one sync entry of up to 16 MiB,
JSON-escaped); serve handles at most 64 connections at once.

| `op` | Request fields | Answer |
|---|---|---|
| `tools` | `session` | `{tools, digest}`: the session's tool set, empty when unbound |
| `call` | `session`, `name`, `arguments` | `{result: {text, isError}}` when the platform recorded the call's result, or `{error}`; then one acknowledgement byte from `mcp` |
| `watch` | `session` | `{digest}` now and after every change, until either side closes |
| `status` | — | `{lines}`: one status line per binding |
| `disconnect` | `connectionId` | `{}` once the binding is disconnected and removed, or `{error}` |
| `attach` | `connectionId` | `{}` once serve runs the binding that `connect` stored, or `{error}` |

Any refusal is `{error: "<reason>"}`.
