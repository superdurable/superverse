# Superverse V2 documentation

The [architecture specification](architecture/superverse-v2.md) describes the current platform contract. Each durable fact has one owning document; other documents link to it.

| Area | Current documentation |
| --- | --- |
| [Architecture](architecture/superverse-v2.md) | Service boundaries, durable owners, Flow contracts, isolation and acceptance requirements. |
| [Agent harness](architecture/agent-harness-design/README.md) | Session, model and context, tools, Sandbox and Preview, subagents, connectors, notifications, commit gate, application tests, streaming and Studio. |
| [TeamSpace](architecture/teamspace/README.md) | TeamSpaces, Project roles, shared connections and the PostgreSQL read model. |
| [Remote agent](architecture/remote-agent/README.md) | A Builder's coding agent as the Session executor, shared Sessions and failure feedback. |
| [Design before implementation](architecture/design-first/README.md) | Flow design on the requirements card and its view in Studio. |
| [Studio setup](architecture/studio-setup/README.md) | Configuration drafts, Preview apply, publishing, status and events. |
| [API](api/resource-grpc.md) | Business services and transport contracts, including the [Agent wire contract](api/agent-protobuf-contract.md), [credential read endpoint](api/credential-read.md) and [remote agent endpoint](api/remote-agent.md). |
| [Flow graphs](flows/README.md) | Generated definitions for platform Flows. |
| [Runbooks](runbooks/README.md) | Local operation and development guidance. |

The [StartFlow admission contract](architecture/start-flow-admission.md) defines FlowID reuse and admission. [Runs cache and Watch authorization](architecture/runs-target-cache-and-watch-authorization.md) defines Studio's read and access behavior. Component READMEs stay beside their code. The [Dex application skills](../dex-skills/README.md) describe the guides bundled for the Coding Agent.
