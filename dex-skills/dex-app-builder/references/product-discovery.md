# Product discovery

Resolve the business model from the request, retained answers and host context.
A clear request to build the product authorizes routine implementation choices.
Record reasonable defaults and continue; ask only when an unresolved business,
authorization or integration decision materially changes the outcome. Do not ask
users to choose SDK methods, compile fixes or another approval of the same scope.

## Stack checkpoint

Classify the repository before proposing a language or installing anything. A
new Superverse project starts from the platform's go-react-v1 application
template with `dex-app.yaml`. Record the
template stack as the stack decision; do not offer TypeScript, Node or another
SDK language as equivalent defaults.

Superverse checks, previews and publishes only the go-react-v1 build profile:
a Go Dex backend, which strict FDG 2.0 and the Connector SDK use, and a React
frontend. If the user asks for a non-Go Dex backend, explain that it cannot be
built or run here instead of changing the stack.

## Actor, role, operation, and permission matrix

Inventory actors first, then derive the minimum authorization model. Start
with one `admin` role for the trusted maintainers and operators. A requester,
subscriber, participant, or external system is an actor, not automatically a
platform role.

| Actor | Authentication boundary | Goal and visibility | Actions and permissions | Platform role | Evidence for another role |
| --- | --- | --- | --- | --- | --- |
| Trusted maintainer or operator | authenticated host or reverse proxy | all required operational state | granular approval, edit, recovery, and configuration permissions | `admin` | none by default |
| Requester or participant | product-specific | only confirmed participant-facing state | typed start, response, or Action when required | none by default | a distinct authenticated membership boundary plus different visibility or allowed operations |
| External system | Connector or integration identity | only the required exchange | typed Trigger, RPC, or event | none | never derive a human role from an integration identity |

Add another role only when a confirmed, separately authenticated group needs a
different view or a different set of allowed operations. Do not create one role
per actor, lifecycle stage, Action, or permission. Keep Action permissions
granular even when `admin` initially holds all of them. Record authentication,
authorization, audit, and reason requirements, and justify every non-`admin`
role in the confirmation artifact; collapse an unjustified role back into
`admin`.

## Lifecycle questions

Confirm:

- business identity and unique Flow ID source;
- start trigger and duplicate-start behavior;
- typed start input and completion output;
- happy path and terminal business outcomes;
- human decisions, required fields, deadlines, reminders, and escalation;
- retryable provider failures versus business rejection;
- unknown external mutation outcomes and query-first reconciliation;
- cancellation, recovery, cleanup, and operator intervention;
- searchable/indexed fields and detailed display fields;
- sensitive data that must not enter IDs, logs, Streams, or generated artifacts.

## Flow-boundary decisions

Treat three shapes as distinct decisions:

- parallel Steps share one Flow identity and lifecycle;
- independent top-level Flows start separately and coordinate through typed
  RPCs or Channels;
- SubFlows have an explicit parent-child lifecycle.

First capture the data-lifecycle boundary:

| Candidate data or process | Authoritative owner | Retention and cleanup | Independent waits, Timers, and terminal outcomes | Pollution if kept in the existing Flow | RPCs or messages between the Flows per frequent event | Top-level Flow decision |
| --- | --- | --- | --- | --- | --- | --- |
| Product-specific | stable business owner | product-specific | product-specific | unrelated state the existing Flow would retain or coordinate | none, or the split is wrong | keep or split, with evidence |

Use these questions together:

1. Does the data have a different authoritative owner and a different
   retention or cleanup lifecycle?
2. Does the process have its own waits, Timers, and terminal outcomes such as
   `verified`, `expired`, or `cancelled`?
3. Would putting it in an existing Flow force that Flow to retain, clean up, or
   coordinate state unrelated to its primary lifecycle?
4. Would the split make the Flows exchange RPCs or Channel messages on frequent
   events, such as every user operation, every tool call or every poll?

A yes to question 4 rules the split out. Every message between Flows is
recorded in both executions' history, and every RPC, including a read-only one,
is a separate engine operation with its own latency and cost. A separate Flow
that tracks another Flow's inactivity, for example, needs a notification from
every operation; keep that deadline in the Flow where the operations happen.
Calls between Flows are fine for rare events: a start, a completion, a
cancellation or an explicit user command.

When the answers to questions 1 to 3 are collectively yes and the answer to
question 4 is no, create an independent top-level Flow.
Otherwise keep the work in the existing Flow's Steps, Attributes, or
AttributeMaps. Field count, source size, number of stages, retry policy, code
reuse, provider neutrality, or ordinary parallelism do not establish a new
top-level Flow. Keep long-lived authoritative facts in their stable owner and
temporary validation or failure state in its short-lived owner; temporary
state must not pollute the authoritative store.

Then capture the execution-shape decision:

| Candidate work | Same business lifecycle | Parallel-Step or batching option | Observed complexity or more than 200 concurrent Step executions | Explicit SubFlow confirmation | Decision and rejected alternatives |
| --- | --- | --- | --- | --- | --- |
| Product-specific | yes or no | concrete option | measured requirement, not speculation | required before SubFlow implementation | Step, independent top-level Flow, or confirmed SubFlow |

New applications default to no SubFlows. Use static or dynamic parallel Steps,
Channels for joins or quorum, batching, typed RPCs, and ordinary language
helpers or interfaces first. Provider abstraction, research stages, model
calls, code reuse, an independent retry policy, or ordinary bounded fan-out do
not justify a SubFlow.

Propose a SubFlow only after all of these are true:

1. an existing single-Flow design or running system provides evidence, rather
   than a concern that it may become complex later;
2. the main graph is already impractical to review, evolve, or operate, or one
   fan-out genuinely requires more than 200 concurrent Step executions;
3. parallel Steps, batching, Channel coordination, RPCs, and ordinary code
   abstraction have been evaluated and rejected with reasons;
4. child identity, input/output, parent completion, cancellation, failure,
   retry, duplicate submission, and concurrency semantics are defined; and
5. the user explicitly confirms the SubFlow design.

The 200-Step value is an architecture-review threshold, not a claimed Dex
Server limit. Crossing it permits a SubFlow proposal; it never selects one
automatically.

One shape skips items 1 to 3: a long-running polling Execute that would run
beside other ongoing work in the same Flow. Propose it as a single-child
SubFlow from the start, as [a long poll beside other
work](../../dex-sdk/references/core/patterns.md#a-long-poll-beside-other-work)
explains; items 4 and 5 still apply.

## Storage decision

Default each durable fact to Dex before proposing an external dependency.
Capture a storage decision matrix:

| Fact or collection | Owning Flow/business identity | Retention and cleanup | Reads and writes | Volume/contention | Dex primitive and access path | Proven external-store gap |
| --- | --- | --- | --- | --- | --- | --- |
| Process state | Process Flow ID | follows the process lifecycle | Step and Action updates, status/detail reads | product-specific | typed Attribute plus RPC/index when needed | normally none |
| Keyed or growing records | Stable domain/entity owner | explicit record retention and deletion | exact lookup, bounded page, independent mutation | product-specific | partitioned or chunked AttributeMap with exact loads and locks | only a confirmed query or contention limit |
| Shared domain facts | Stable domain/entity Flow | owned independently of callers | reused by several process/API paths | product-specific | one owner with typed application operations | cross-Flow reuse alone is not a gap |

Do not select PostgreSQL, another database, a cache, an ORM, or a shadow read
model merely because data must persist. Dex already provides durable
Attributes, AttributeMaps, indexes/search, locks, exact instance loading, and
blob offload. Superverse project Dex engines run without an Attribute Store, so
Attribute Store projections are not available. Require an external store only for a
specific unsupported need such as complex/ad-hoc indexes, full-text or vector
search, high-concurrency access to one hot record, relational joins or
multi-record transactions, or large analytical scans. Record the exact
operation, expected scale/SLO, authority, synchronization, failure, and
reconciliation design. “We may need it later” is not evidence.

## Management UI capability mapping

Business process products commonly need a management interface. Design that
interface through Superverse Studio first. For each management operation, define its
Indexed Attributes, Summary RPC fields, Display RPC fields, Action RPC input and
metadata, condition, and permission before deciding that a custom surface is
necessary.

| Management need | Studio capability |
| --- | --- |
| List and filter business records | Model each record as a Run, name it with a Display title slot built from business data (such as `Refund R-1042 · Acme · $320`), expose supported search criteria as Indexed Attributes, and return additional list fields from the Summary RPC. |
| Inspect one record | Return detail fields and UI slots from the Display RPC. |
| Perform a business operation | Register an RPC with Action metadata, a typed input form, current-state condition, and one required permission. |
| Find assigned or actionable work | Inbox discovers Runs from Action permission history; opening a Run rechecks current Action eligibility. |
| Change simple business fields | Declare editable scalar Attributes in the Display RPC. |
| Inspect progress and failures | Use the Run timeline, Step graph, execution details, and recovery information. |

Record this mapping as a discovery artifact. If a need is fully covered, keep
it in Studio even when another need requires Custom UI. If it is not
covered, name the exact missing interaction and its user. A general desire for
an admin portal, dashboard, management backend, or branded shell is not a
capability gap.

## UI-mode decision

Make the UI-mode decision only after completing the management UI capability
mapping and designing the Summary RPC, Display RPC, Action RPCs, Indexed
Attributes, and permissions.

Choose **No custom UI** when those surfaces satisfy operators and maintainers.
Keep only the template's non-business server/OpenAPI architecture for
future evolution. Confirm whether the existing host supplies authentication,
role-to-permission mapping, project/tenant isolation, and a trusted reverse
proxy. Those controls may still be required, but they are not a second process
management backend.

Choose **Custom UI** only for a recorded Studio capability gap, such as a
participant-facing journey, a fundamentally different navigation model, domain
visualization, or a complex interaction the platform does not provide. Record
which requirement forces the custom surface.
When consequential interaction choices remain unresolved, an optional UI
checkpoint clarifies the necessary pages, navigation, fields and actions through
a low-fidelity static wireframe. A clear implementation request already supplies
confirmation for its stated behavior. It does not authorize an
interactive mock, visual system, imagery, animation, or other polish before the
Flow, Connector, and OpenAPI contract are designed.

## Connector decision

Create a connector capability matrix for every Trigger, Query, Mutation, and
integration UI. Search the published catalog with `connector_catalog`. For a
public external product, match each need to an exact released connector
capability—not merely a connector name—then run the returned `install_command`
and verify the complete contract in that release's `connector.yaml`
(directory: `go list -m -f '{{.Dir}}' <module path>`). Record the connector ID, capability
kind and name, module path and version, and reuse or gap decision.

If the catalog or the release's `connector.yaml` cannot be verified,
stop connector-dependent implementation and report the blocker. A missing
connector, operation, Trigger, or UI unit with a documented API or official SDK
is a capability gap to report to the user as a blocker; it is not permission to
install the provider SDK or write local integration code.

Mark a service internal only when the organization owns and controls it.
`dex-app.yaml` accepts only published modules under
`github.com/superdurable/dex-connectors-library/connectors/`, so an internal
connector library cannot be used here, and the catalog has no generic HTTP
connector. A controlled internal service, like any sender or receiver without a
dedicated connector, can send webhook requests that the application verifies
and receive its signed events through the generic `webhook` connector
(`requestReceived` Trigger, `sendEvent` Mutation); any other internal-service
need is a capability gap to report.

## Confirmation artifact

Confirm a new application or a large change with the user once, before code, with the
requirements card (`confirm_requirements`): the features, the connections to external
systems, what the user must prepare and how acceptance works, in business language. A
large change alters the confirmed requirements: it adds, removes or changes a feature or
a connection, or changes what the user sets up or the acceptance. Its revised card keeps
every confirmed feature's number and never reuses a removed one; a completely different
request gets a card that replaces the requirements and numbers from R1 again. Make a
small change, one that leaves the confirmed requirements as they are (copy, styling,
layout, a bug fix that restores confirmed behavior, refactoring), directly, and never ask
again for a version the user confirmed. The design record below stays in the
application's owning documentation; it is not a second confirmation for the user.

A card that adds, removes or changes a Flow's Steps, waits, transitions, Channels,
Actions or the Flows it starts also carries the Flow design (`design`): each Flow it adds
or changes, listed in full after reading the current source of a changed one, with the
Go type names to implement. Studio draws it for the user until the build is complete,
with the split and the groups the Flow canvas applies to built Flows, so the design
follows the rules of [FDG authoring](fdg-authoring.md):

- A group of two or more main-story Steps is one run, where ordinary transitions inside
  it join all of its Steps, or one choice, where one Step outside it leads into every
  part.
- Set `is_failure_route` only for breakage, such as exhausted retries or an uncertain
  outcome. Business rejections and connector outcome branches stay ordinary transitions
  and stay in the main story.

After the push that completes the build, compare the built Flows with the confirmed
design and name every difference in the report to the user, or say that the design was
built as confirmed.

Before code, record:

1. the actor-to-role-to-permission matrix, with every non-`admin` role justified;
2. a numbered lifecycle with decisions and terminal outcomes;
3. the data-lifecycle and execution-shape boundary matrices, including every
   independent top-level Flow, every candidate collapsed into a Step, RPC, or
   helper, and the complete gate for any proposed SubFlow;
4. the management UI capability mapping, including proposed Summary, Display,
   Action, Indexed Attribute, Inbox, and permission contracts;
5. the confirmed **No custom UI** or **Custom UI** mode and any recorded Studio
   capability gap;
6. proposed Flow, Step, state, message, timer, RPC, and connector boundaries;
7. the storage decision matrix and any evidence-backed external-store gap;
8. connector capability reuse, selected module versions, and any reported
   capability gap;
9. unresolved tradeoffs.

A clear implementation request authorizes the work; the requirements card confirms
what a new application or a large change will be. A casual discussion response is not
approval to implement.
