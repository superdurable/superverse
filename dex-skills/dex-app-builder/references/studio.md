# Superverse Studio and FDG 2.0

Superverse Studio is the application's management interface. Build **Design**
renders the strict FDG 2.0 definitions that `dexcli visualize` produces for the
Flow files in `dex-app.yaml`; **Configuration** holds connector connections and
credentials; **Preview** runs the prepared commit's application and Worker on
the Preview engine inside the coding Sandbox; **Runs** and **Inbox** show
Preview or Live;
**Goals** shows the goals file of the Live build with the values that reviews
measured from the runs of the Live, published version only; on Preview it
shows no values and says goals are measured on Live runs, because Preview and
test runs never count toward a goal ([goals](evolve.md));
and **Publishing** builds and deploys Live from a committed main-branch source.
There is no Dex Web, connection file or connector UI iframe. FDG 2.0 management
metadata is Go-only: validate every Flow with the 2.0 analyzer and never fall
back to 1.0.

## Run and Work Queue surfaces

Studio derives Runs and Inbox (its Work Queue) from:

- indexed Attributes for Run list columns and filters;
- `GetDexSummary` for additional read-only list fields;
- `GetDexDisplay` for Run detail fields, UI slots and editable fields;
- Step groups and explanations for the canvas and Step details;
- Steps waiting on a Channel that an RPC publishes to, shown as human gates
  answered by that RPC;
- eligible Action RPCs for operator operations.

A Run list row shows the Display `title` and `status` slot values when that
Attribute is indexed or also returned by Summary; otherwise it shows the
process name with the start time and the engine status. A title slot is
expected, not optional: every Flow a person sees declares one built from
business data, such as `Refund R-1042 · Acme · $320`
([syntax](fdg-authoring.md#summary-and-display)). The Run detail shows slotted
Display fields first, in slot order.

## Management UI design

Treat Studio as the first management interface, not merely a Flow inspector.
Model one business record as one Run. Use Indexed Attributes for supported
filters, the Summary RPC for additional list fields, the Display RPC for detail
fields and UI slots, and RPC Action metadata for buttons, typed input forms,
current-state conditions, and permissions. Use editable scalar Display
Attributes for simple updates; Studio writes them directly while the Run is
active, without calling a Flow RPC. Inbox discovers Runs from cumulative Action
permission history, then the opened Run rechecks current Action eligibility.
Run diagnostics (timeline, execution graph, Stop and Time Travel) cover process
progress and failures.

Design those contracts before proposing a custom management backend or UI. A
custom surface requires a recorded interaction Studio cannot provide; the fact
that a product needs administration does not establish such a gap.

Studio authorizes the signed-in Project owner or administrator on its server
and grants every Action permission the FDG declares plus `flows.start`; there is
no role or permission selector. Inbox lists open Runs whose Work Queue
permission history matches those permissions. An opened Run offers only Actions
whose permission and current condition match, and the server rechecks the
condition before invoking the RPC.

## Start Flow

Runs shows **Start Flow** when the definition has a supported typed Start
schema ([Start input](fdg-authoring.md#start-input)) and the selected Preview or
Live target accepts starts. The user enters business input only. Studio
generates the Flow ID (`flow-<UUID>`) and request identity, so it cannot start a
Flow under a business-derived Flow ID; such a Flow needs its own start path in
the application. After an uncertain response, Studio offers **Recover Start**
with the same identity and input.

Studio derives `SkipWaitFor` from the Start Step's phase in the validated
graph: `execute` skips WaitFor, while `wait_for+execute` retains it. A missing
or unknown phase is an invalid definition. Do not rewrite an execute-only
application Step into a dummy WaitFor to work around admission.

Start, Runs search and per-Step Configuration use the FDG's Flow and Step type
names, which must match the Worker's registrations. Follow the SDK's
[default type-name rule](../../dex-sdk/references/go/versioning.md#default-flow-and-step-type-names).
Keep derived names for new definitions. If the graph and Worker names differ,
diagnose the SDK and CLI versions or generated metadata; do not implement
`GetFlowType` and `GetStepType` on every Flow and Step as a workaround.

Connector Trigger applications still use the real Trigger for their Trigger
acceptance path.

## Connector configuration

Studio **Configuration** renders each connection declared in `dex-app.yaml`
from that exact connector release's manifest and the FDG's connector Step
metadata. Studio renders the fields and provider pickers itself. The user enters
credentials there; application source never contains them. A connector Step is
configurable only when it uses an operation-specific factory from an exact
released official module with a static `ConnectionName` equal to the
`dex-app.yaml` `connectionName` (see the connector diagnostics in
[FDG authoring](fdg-authoring.md#focused-verification)).

Connection credentials and Trigger matcher configuration are separate records.
Changing one binding does not change another Flow that reuses the same
connection. Credentials are resolved for each provider call, so a credential
change applies to the next Connector call. Ordinary connection, binding and
operation settings are loaded when the application starts, so they apply to the
next Preview start or Live build. Preview and Live keep separate configuration,
and both require configuration that validates against the exact manifest;
missing credentials do not block a source commit. An application consuming a
released connector must not add duplicate configuration for values the
connector derives or lets the user pick.

## Source layout

Keep each Flow in one Go file as described in
[file and registration boundaries](fdg-authoring.md#file-and-registration-boundaries).

## Directives

Use the focused [Go FDG authoring reference](fdg-authoring.md)
for exact field, input, indexed Attribute and typed Action syntax. It owns the
syntax examples Studio relies on.

### QR capture hint

A user-sourced string Action input may add `capture:qr-code` to its `dex:input`
directive. FDG 2.0 emits `capture: "qr-code"`. No other capture value is
supported, and Attribute-sourced or non-string fields fail analysis. Omitting
the hint preserves the ordinary text input.

Studio keeps that text input and adds **Scan QR code**. The scanner prefers the
rear camera, needs a secure context and camera permission, and copies the
decoded text into the field without submitting the Action; manual input remains
available. Scanning does not authenticate the operator, grant a permission,
satisfy an Action condition, validate the business value, or bypass the RPC.

## Permission projection

The Go Worker includes the complete Action permission mapping only when a successful WaitFor, Execute, timeout Execute, or RPC invocation writes or deletes an Action condition source. Unrelated writes, failed invocations, and query-only RPCs omit it. Initial Flow and SubFlow state receives its projection directly from the Go SDK, including RPC-only Flows.

The Server overlays those business writes on authoritative Attribute state, evaluates every mapping, sorts and deduplicates newly matching permissions, and atomically adds them to `DexWorkQueuePermissions`. Once matched, a permission remains in that Flow's Work Queue permission history and remains searchable after completion. Empty mappings and later state changes do not remove history. Application Steps do not need projection-only Attribute locks.

Studio Display edits follow the same rule: only an edit that modifies an Action condition source includes the complete mapping. An Action definition is a stable contract for an active Flow. A changed definition is evaluated on a later source write or a new Flow. Removing or renaming a permission does not clear existing history.

Permission history is discovery data, not authorization evidence or proof that an Action remains eligible. Every Action RPC rechecks current business state. Studio's server authorizes each Action against the caller's permissions.

## Validation

Use the [Go FDG validation and diagnostic reference](fdg-authoring.md#focused-verification).
Validate every Flow declared in `dex-app.yaml` with `make check-fdg-v2`. Studio
Design and Preview use the definitions prepared from the pushed commit; never
start a second Dex engine in the Sandbox.
