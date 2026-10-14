# Application stack and repository bootstrap

A Superverse project repository starts from the platform's go-react-v1
application template. Read its `AGENTS.md` first: it names the exact files to
change and the reference Flow to copy. Inspect `.superverse/template.json`,
`go.mod`, `dex-app.yaml` and the Makefile before changing dependencies, and use
`make bootstrap` as the first dependency command.

Dex Server is backward compatible with every earlier SDK release, and the
platform keeps its Dex Server at the newest stable release. The application is
not pinned to a platform-chosen Dex Go SDK, Connector SDK, connector or Dex CLI
version; `go.mod` records the minimum versions it builds with. Move any of them
to a newer stable release when useful (`go get <module>@latest`), after reading
that release's "Breaking Changes" section, and keep `go.mod`, `go.sum` and
`dex-app.yaml` connector versions consistent. Use released versions only: no
prereleases, pseudo-versions or `replace` directives. The `go.mod`-selected
module source and the compiler are the API authority.

Keep every Flow source and static connector connection in `dex-app.yaml`; never
put configuration values or credentials there.

Do not invent a different application stack. Do not start a TypeScript or Node
Dex backend, create an ad hoc npm application, or copy bootstrap code from
another project. TypeScript is limited to the template's React frontend and
generated client. The Dex backend and application-side Connector SDK
integration are Go-only.
