# Real Local Kind acceptance

Use `./infra-as-code/sv2 kind e2e` after build/deploy/ports and the real builder.
Only actual Studio/BFF/gRPC, Dex/Temporal, the Git host (the local Git host, a real Git
service, or GitHub), model, Docker build, scope storage, Preview/Live and external provider
APIs qualify. No unit or fake-provider test is
an acceptance substitute.

The required scenarios are `registration.spec.ts` (registration/login/logout),
`create-project.spec.ts` (create/Agent UI change/Preview/release/Live replacement/removal/delete) and
`template-approval.spec.ts` (an Agent-added human approval Step and Action, then
start/approve/complete in Preview and Live, Live removal and delete); see the
[E2E contract](../../../../web/studio/tests/e2e/README.md). The runner refuses to
emit a full receipt until all three real scenarios pass on the accepted
source/runtime.

The default profile (`local-kind.yaml`) needs no GitHub credentials: the runner passes the
deployed profile's repository host to the specs. The final acceptance profile
(`local-kind-acceptance.yaml`) reuses the existing approved GitHub/model credentials through
ignored private configuration. The cases also read `SUPERVERSE_E2E_OPENAI_FILE` for the template's
summary connection. No case needs an operator. Do not restore simulated provider
endpoints to satisfy this gate. Historical receipts describe only the coverage
that was actually run.
