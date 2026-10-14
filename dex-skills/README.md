# Dex application skills

These Superverse-owned guides help the Studio Coding Agent build users' Dex
applications. The [Agent harness contract](../docs/architecture/agent-harness-design/README.md) describes how the platform uses these guides.

- [Dex App Builder](dex-app-builder/SKILL.md) covers application authoring,
  template conventions, Studio integration and source handoff.
- [Dex SDK](dex-sdk/SKILL.md) covers Go Flow implementation, primitives, Client
  calls and recovery.
- [Dex Connector Contributor](dex-connector-contributor/SKILL.md) guides a
  `connector_develop` subagent that builds or fixes one connector of the public
  connectors library in a worktree the platform prepared. It is adapted from the
  library's public contributor skill, and it is also the end of that subagent's
  system prompt: `internal/agents/session/conversation` embeds a copy, which
  `make generate-go` refreshes and `make check-generated` checks.

## How the Agent reads them

1. The [Coding image](../sandbox/coding/Dockerfile) copies this directory to
   `/opt/superverse/dex-skills/` inside the Sandbox.
2. [Repository context](../internal/agents/sandbox/workspace/git/repository_context.go)
   advertises the App Builder entry path and includes the template's `AGENTS.md`
   file map. [Session prompt preparation](../internal/agents/session/session_workspace.go)
   appends this context to the Agent's system prompt. Skill contents are not
   automatically inserted.
3. The Agent reads relevant files on demand with its file tools, which treat
   the installed bundle as a read-only root. For example, `read` accepts:

   ```json
   {
     "path": "/opt/superverse/dex-skills/dex-app-builder/SKILL.md"
   }
   ```

   `grep` and `glob` search the bundle with the same path.

The [file helper](../sandbox/coding/cmd/superverse-files/README.md) resolves the
path, symlinks first, against the agent root and the read-only roots and returns
numbered lines, at most 2,000 lines and 64 KiB, with the offset to read on from.
The bundle is read-only: a write, and any path that leaves the roots, is refused.
Mentioning a path in the prompt does not guarantee it is read.

## Why this is outside the application template

[app-template/](../app-template/) becomes each user's application repository;
these guides belong to the platform's Coding image. Keeping them here avoids
copying a separate documentation snapshot into every application. The template's
`AGENTS.md` points to the installed references by their `/opt/superverse/dex-skills` paths.
The skill bundle stays outside the application workspace and must not be copied
or checked into the user's application repository.
