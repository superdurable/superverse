# Superverse

Superverse is a platform where a coding agent builds [Dex](https://github.com/superdurable/dex) applications. Studio is the workspace for creating and managing projects; Preview runs an application during development, and Live serves a deployed version.

The Go server and platform services live in `cmd/` and `internal/`. Studio is in `web/studio/`, the coding sandbox in `sandbox/`, the generated application source in `app-template/`, and local infrastructure in `infra-as-code/`. API definitions are in `api/`.

To run Superverse locally with the Local Native lane, follow the [Local Native runbook](docs/runbooks/local-native.md). Start with the [documentation index](docs/README.md) for the architecture and API contracts.

Superverse is licensed under the [Sustainable Use License 1.0](LICENSE). `app-template/` and `dex-skills/` are MIT licensed; see [LICENSING.md](LICENSING.md).
