# Application template

<!-- BEGIN GENERATED PACKAGE SUMMARY (scripts/check-package-readmes.py; edit only Purpose) -->
Purpose: Embeds the go-react-v1 application template that Project setup pushes into new managed repositories, with its revision and locally computed Git object IDs.

Callers: `cmd/tools/app-template-archive`, `internal/agents/session/modelprovider/mockmodel`, `internal/api`, `internal/platform/repohost/github`, `internal/platform/repohost/localgit` and `internal/projects/projecttest` import this package.

Dependencies: it imports `internal/comm`.

```mermaid
flowchart LR
  self["internal/app/apptemplate"]
  c0["cmd/tools/app-template-archive"] --> self
  c1["internal/agents/session/modelprovider/mockmodel"] --> self
  c2["internal/api"] --> self
  c3["internal/platform/repohost/github"] --> self
  c4["internal/platform/repohost/localgit"] --> self
  c5["internal/projects/projecttest"] --> self
  self --> d0["internal/comm"]
```
<!-- END GENERATED PACKAGE SUMMARY -->

Serves the go-react-v1 application template that new managed repositories start
from. The template source lives in `app-template/`; `make generate-app-template`
(`go generate ./internal/app/apptemplate`) packs its committable files into the
embedded `app-template.tar` and renders the matching Studio initial design.
`Current` returns the embedded template with its revision, `app-template@`
followed by the Git tree ID, which the package computes locally so that Setup can
verify the objects it created through the GitHub API.

| File | Responsibility |
| --- | --- |
| `application_template.go` | The embedded archive, `Current`, `Decode` and the template revision |
| `git_object_id.go` | Git blob and tree object IDs |
