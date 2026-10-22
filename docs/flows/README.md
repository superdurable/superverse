# Platform Flow graphs

The JSON files in this directory are generated FDG 2.0 for the sixteen platform
Flows: RepositoryBinding, Project, ProjectSetup, SessionAgent, Subagent,
PlatformIssue, ConnectorContribution, BackgroundCommand, SessionWorkspace, SandboxProvision, Preview, ProjectRelease, ProjectDeploy, TeamSpace, Attachment and Evolve. Their detailed identities, primitives, Maps,
locks, channels, streams, Steps, RPCs, retries and cleanup contracts are in the
[architecture specification](../architecture/superverse-v2.md#dex-flow-changes);
the agent's Flows are described in the
[agent harness design](../architecture/agent-harness-design/README.md#the-pieces), and each
package README next to a Flow owns its complete contract.

Render changed Flow source with the repository-pinned Dex CLI and retain its
schema version, validity and diagnostics in the relevant implementation receipt.
A valid graph does not prove external effects or complete Local Kind acceptance.
Connection and OAuth lifecycles use the native Go configuration host, scoped
versioned storage and the application SDK; they are not additional platform Flow
graphs.

Business owner source paths are recorded in the
[package tree decision](../architecture/superverse-v2.md);
the unchanged-persistence review of the original package split is in the
[business package ownership decision](../architecture/superverse-v2.md).
Pass `--schema-version 2.0` explicitly: the pinned CLI defaults to 1.0.
All sixteen checked-in graphs declare 2.0 and include the actual search,
summary, display and start definitions. The root graph check also requires this
version; CLI branding alone does not select it.

Flow and Step names derive from the concrete Go type name without its package.
There are no platform identity overrides. The pinned SDK registry and
all sixteen graphs are compared directly; the identity decision
defines the pre-launch history boundary.

`make check-flow-definition` regenerates all sixteen registered owners with the
pinned CLI, requires exact source/type identity and zero diagnostics, and compares
the native JSON bytes against these files. It also checks the Session, Subagent, SessionWorkspace and
Preview Channel wait edges, the Subagent's stop, read and cleanup index, Release build
AttributeMap/RPCs, and Deployment build identity. Use
`python3 scripts/check-flow-definitions.py /path/to/pinned/dexcli --write` to
refresh the checked-in graphs after an intentional Flow change, then rerun the
check. This is static drift detection, not business integration acceptance.

The CLI derives the graphs from the source and never builds the SDK registry.
`make check-flow-registry` (part of `make check-go`) builds it from all sixteen
Flows as the server bootstrap does, so a Flow the SDK refuses at registration,
or StepOptions that do not resolve, such as an Execute failure route that leads
back to its own Step, fails the check rather than the server's start.
