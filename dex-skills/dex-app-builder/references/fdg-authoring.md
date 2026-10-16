# Go FDG 2.0 authoring

Use this page for Summary, Display, Start input, Inbox Actions and analyzer
diagnostics before searching the CLI implementation. The sandbox's
`dexcli visualize` and the application's `go.mod`-selected SDK are the
authority for this syntax. Superverse Studio reads these definitions for
Design, Runs, Inbox and Start Flow ([how Studio uses them](studio.md)); no
other management server is needed.

## File and registration boundaries

Keep one Flow per Go file with its Steps, Attributes, Channels and other Dex
resources, view and Action RPCs, Action input structs and `dex:*` directives.
Register every durable primitive in `GetPersistenceSchema`, every Step in
`GetSteps`, and both view RPCs in `GetRPCs`. Use the SDK's default Flow and Step
type names (the Go type name without its package): name Flow types after their
domain and keep them unique; a generic type has no default name. See
[default type names](../../dex-sdk/references/go/versioning.md#default-flow-and-step-type-names).
Connector factory Steps use their generated configuration's static `StepType`,
`ConnectionName` and `Annotations`; inspect that exact released factory type.

Each ordinary Step type has one group and explanation. `group-id` is
kebab-case, and Steps that share a `group-id` use the same label:

```go
// dex:group group-id:process group-label:"Process"
// dex:explanation text:"Perform the accepted operation."
```

A group of two or more Steps does one thing, so the Flow canvas can fold it
into one stage that opens in place. Make it one run, where ordinary transitions
inside the group join all of its Steps, or one choice, where one Step outside
the group leads into every part, such as an agent's decision Step and its
tools. From `dexcli` v1.8.0 the analyzer warns about any other group with
`v2_step_group_shape` and names the parts it falls into; the canvas then shows
those Steps in the group's band instead of folding them.

Groups are judged on the Flow's main story. A Step that only failure
transitions reach, directly or through other such Steps, is failure handling:
the canvas leaves it out of the drawing, counts the failures on the Step card
they leave, and draws them beside that Step when it is selected or a run went
through them. A failure transition joins no group, and a failure-handling
Step's group is not checked, so give it the group of the Step it handles.

When a Step only records breakage, such as exhausted retries or an uncertain
outcome, make it the failing Step's Execute failure route
(`ProceedToOnExecuteFailure`) rather than an ordinary decision, so that the
canvas shows it as failure handling. Business rejections and connector outcome
branches stay ordinary decisions and stay in the main story.

Every indexed Attribute also has a matching directive:

```go
// dex:indexed-attribute attribute-key:status index-key:keyword2 index-type:keyword value-type:string description:"Current status"
var Status = dex.DefineAttribute[string]("status",
    dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword2"}))
```

## Summary and Display

Both methods accept `(ctx dex.Context, _ dex.None)` and return
`(*dex.RPCResult[map[string]any], error)`. Register them through
`dex.DefineRPC(flow.GetDexSummary, nil)` and the corresponding Display method.
Both are read-only: read only the declared Attributes and never write,
publish, move or cancel Steps. Return a map literal with exactly the field keys
declared on that method, including null for a missing value; Studio also
rejects other keys or mismatched value types at runtime. Do not build the map
through loops or helpers that hide its keys from static analysis.

```go
// dex:field attribute-key:status value-type:string editable:false description:"Current status" ui-slot:status
func (*ProcessFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
    status, err := Status.Get(ctx)
    if err != nil { return nil, err }
    return &dex.RPCResult[map[string]any]{Output: map[string]any{"status": status}}, nil
}
```

The exact arguments are `attribute-key`, `value-type`, `editable`,
`description`, and optional `ui-slot`. There is no `field-key`, `rpc`,
`dex:display`, or implicit label syntax. A field refers to an Attribute declared
in this file, not an arbitrary computed map entry. An empty view returns an
empty map and has no field directives. Do not repeat indexed Attributes in
Summary; they already appear in the list. Display may include them.

`value-type` is the analyzer's name for the Attribute's Go type: `string`,
`bool`, `int64`, `double` (`float64`), `datetime` (`time.Time`), `string-array`
(`[]string`), `array` (other slices), `object` (an unnamed struct or map) and
`attribute-map`. Every other type is `json`, including `int` and named types
such as `type Status string`.

Use `ui-slot:title`, `subtitle`, `status`, `recommendation`, or `reason`.
Each slot except `reason` is unique within one view. Ordinary fields need no
slot.

Every Flow a person sees in Runs or Inbox declares a `ui-slot:title` Display
field. Its Attribute is a `string` the Flow sets from business data as soon as
it has it, such as `Refund R-1042 · Acme · $320`, never the Flow ID or a status,
and Summary also returns it (or it is indexed) so the run list can name each
run. Without one, Studio names a run by its process and start time. Summary is always read-only. Editable Display fields must be `string`,
`int64`, `double`, `bool` or `datetime`. Keep structured business results in
their owning Attribute and expose its actual FDG type instead of duplicating
business state for presentation.

## Actions and input

An operator Action is typed RPC registration, not a comment directive:

```go
dex.DefineRPC(flow.SubmitDecision, &dex.RPCOptions{
    Action: dex.DefineAction("Submit decision",
        dex.WhenAttributeMatches(Status, dex.AttributeMatchEqual("PENDING")),
        dex.ActionRequiresPermission("process.review")),
    LockAttributes: []dex.AttributeLock{dex.LockAttribute(Status)},
})
```

The label, permission and condition values are compile-time constants. Declare
exactly one permission made of lowercase segments separated by `.` or `-`. The
condition uses a plain `string`, `bool`, `int64` or `float64` Attribute declared
in this file. Do not use removed `dex:action` or `dex:when` comments. The
handler rechecks current state under the business locks required for atomic
admission; the UI condition alone does not authorize or deduplicate a write.
Follow the Flow's accepted revision and operation identity rules.

An Action RPC returns `*dex.RPCResult[dex.None]`. An Action with no fields
accepts `dex.None`. Otherwise use a named same-file struct with one `dex:input`
per exported JSON field, attached to the RPC method:

```go
type DecisionInput struct {
    Decision string `json:"decision"`
    Note *string `json:"note,omitempty"`
}

// dex:input field-name:decision value-type:string source:user required:true description:"Decision"
// dex:input field-name:note value-type:string source:user required:false description:"Optional note"
```

Arguments are `field-name`, `value-type`, `source`, `required`, `description`,
and optional `attribute-key` or `capture`. `source:user` forbids
`attribute-key`. `source:attribute` requires a matching scalar Attribute key;
that input is supplied from the Run and is hidden from the user. Requiredness
must match pointer semantics: optional fields use pointers. QR input adds
`capture:qr-code` only to a user-sourced string field; manual input stays usable.

### Answer a waiting Step

To let a person resume a waiting Step from the Inbox, the Step's `WaitFor`
waits on a Channel (`Decisions.ForOne()`), the Action RPC rechecks state and
publishes the typed decision (`Decisions.Publish(ctx, input)`), and the Step's
`Execute` reads it with `Decisions.GetConditionResults(ctx)`. Call `Publish`
directly in the RPC method so the graph links the Action to that wait; Studio
then shows the Action as the Step's answer. See
[Channel and ChannelMap](../../dex-sdk/references/go/primitives.md#channel-and-channelmap).

The Inbox condition only offers the Action; it neither authorizes nor
deduplicates the write. A complete handler reads the condition Attribute under
its lock, rejects a decision that is no longer current, changes the state and
publishes in the same RPC:

```go
dex.DefineRPC(flow.Approve, &dex.RPCOptions{
    Action: dex.DefineAction("Approve",
        dex.WhenAttributeMatches(ReviewStatus, dex.AttributeMatchEqual("awaiting_review")),
        dex.ActionRequiresPermission("newsletter.review")),
    // The lock also makes the RPC transactional: the read, the new status and
    // the decision commit together or not at all.
    LockAttributes: []dex.AttributeLock{dex.LockAttribute(ReviewStatus)},
}),

type ApprovalInput struct {
    Note *string `json:"note,omitempty"`
}

// dex:input field-name:note value-type:string source:user required:false description:"Optional note"
func (NewsletterFlow) Approve(ctx dex.Context, input ApprovalInput) (*dex.RPCResult[dex.None], error) {
    status, err := ReviewStatus.Get(ctx)
    if err != nil {
        return nil, err
    }
    // A second click, or a decision already queued for this draft, finds
    // another status. The error reaches Studio and nothing is written.
    if status != "awaiting_review" {
        return nil, fmt.Errorf("the draft is %s, not awaiting review", status)
    }
    if err := ReviewStatus.Set(ctx, "approved"); err != nil {
        return nil, err
    }
    if err := Decisions.Publish(ctx, Decision{Approved: true, Note: input.Note}); err != nil {
        return nil, err
    }
    return &dex.RPCResult[dex.None]{}, nil
}
```

Every Action on the same wait locks the same Attribute and leaves the awaiting
status, so at most one decision is queued per wait, and the Step that prepares
the next draft sets the status back to awaiting only after it stored that
draft. An Action without a lock that publishes and writes sets
`IsTransactional: true`.

### Start input

Start Flow is different: its form comes from the input type of the Step
registered with `DefineStartStep`. Use a concrete typed business input with
JSON tags; pointer or `omitempty` fields are optional. Scalars, `time.Time`,
structs, slices, arrays and string-keyed maps can drive the form; interface
values such as `any` (including `map[string]any`), `[]byte`, custom JSON
marshalers and recursive types cannot (`v2_start_input`), and Studio then
offers no Start Flow. Do not add Action input directives to the Start Step or
put Worker addresses, actor permissions, or engine RunIDs in business input.

## Focused verification

Run the template's `make check-fdg-v2` after changing definitions. It renders
every Flow in `dex-app.yaml` with `dexcli visualize` and validates the
`dex-app.yaml` connector and application entries, including that each connector
version matches `go.mod`; it does not deploy or call providers. The direct
equivalent for one file writes `/tmp/flow-fdg.json` (`--out -` prints it):

```bash
dexcli visualize path/to/flow.go --schema-version 2.0 --json --out /tmp/flow-fdg
```

The template check, Preview preparation and Publishing require `valid: true`
and no diagnostic other than the `v2_step_group_shape` warning, so warnings
such as `unused_resource`, `unreachable_step`, `v2_start_input` and connector
configuration warnings also fail them. If analysis fails, read the bounded partial JSON diagnostic before
fetching unrelated source. Compile first when the diagnostic reports Go
syntax/types.

| Diagnostic | Concrete correction |
| --- | --- |
| `v2_view_rpc`, `v2_rpc_signature` | Define and register both view methods with the exact signatures; Action RPCs return `*dex.RPCResult[dex.None]`. |
| `v2_view_rpc_output` | Return one literal map with exactly the declared field keys. |
| `v2_view_rpc_side_effect` | Keep Summary and Display, and local helpers they call, free of writes, publishing and Step movements. |
| `v2_directive` | Match argument names and the Attribute's analyzer `value-type` (see Summary and Display); an unnamed slice is `array` (`[]string` is `string-array`), not `json`. |
| `v2_step_group`, `v2_step_explanation` | Give each ordinary Step exactly one `dex:group` and one `dex:explanation`; a connector factory Step uses `Annotations`. |
| `v2_step_group_shape` | A warning that does not fail the gates: the group is neither one run nor one choice, and the message names its parts. Split it so that each group does one thing; failure-handling Steps are not part of the check. |
| `v2_indexed_attribute` | Give each indexed Attribute exactly one matching `dex:indexed-attribute` on its own `var` spec. |
| `v2_action` | Use direct typed Action registration, a declared plain scalar condition and one permission. |
| `v2_action_input` | Declare every JSON field once; use a pointer for `required:false`. |
| `v2_start_input` | Give the Start Step a statically typed input that can drive a form (see Start input). |
| `generic_flow_type_name`, `generic_step_type_name`, `dynamic_type_name`, `duplicate_step_type` | Use concrete, uniquely named Flow and Step types with default names; an override returns one compile-time string. |
| `multiple_flows`, `*_outside_flow_file`, `step_handler_not_in_file` | Keep one Flow per file and declare its Steps, handlers, RPCs and Dex resources in that file. |
| `hidden_dex_decision` | Return Dex decisions directly from Step methods; helpers may compute input or record state. |
| `unknown_step_target` | Register the target and use its real Go type or exact static Connector Step reference. |
| `connector_factory_step_type` | Give the operation factory a nonempty compile-time StepType. |
| `unused_resource` | Access each primitive from a Step, RPC or timeout handler of this Flow, directly or through same-package helpers, which the analyzer follows up to six levels deep. Never add a no-op call to silence it; delete obsolete scaffold state. |
| `attribute_map_read_not_loaded`, `attribute_map_instance_not_loaded`, `attribute_map_enumeration_not_loaded`, `channel_messages_not_loaded` | Load what the method reads ([Loading state](../../dex-sdk/references/go/primitives.md#loading-state)): in `GetStepOptions`, in `dex.WithStepOptions` on every `GoTo` or `MovementOf` into the Step, in the options of a `ProceedToOnExecuteFailure` route, or in the RPC's `dex.RPCOptions`; enumeration needs the whole map. Otherwise stop reading state the method does not need. |
| `invocation_load_directive` | When another package selects an RPC's map instance with `InvokeRPCWithOptions`, declare `// dex:invocation-load attribute-map:<name>` or `channel-map:<name>` on the RPC method, with the map's Dex name. |
| `step_progress_outside_step` | Write Streams only from Steps. Move the write out of the RPC or timeout handler, including writes through helpers and buffered writers; an RPC publishes a Channel message or returns `NextSteps` to a Step that writes. |
| `start_flow_input_type_mismatch` | Pass `StartFlow` a value of the starting Step's input type. A Step that needs no input declares `dex.None` and the caller passes `nil`. |
| `connector_release_required`, `connector_connection_name_required`, `connector_configuration_unsupported` | Use an operation-specific factory with a static `ConnectionName` from an exact published official connector module (a release `vX.Y.Z` or a release candidate `vX.Y.Z-rc.N`, no `replace`); Studio Configuration cannot configure the Step otherwise. |

A successful FDG check proves a definition, not an actual provider call. Keep
source readiness separate from configured Preview/Live business acceptance.
