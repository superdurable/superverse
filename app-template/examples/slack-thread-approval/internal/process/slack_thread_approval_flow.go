// During Live Worker overlap, put new data in a new Attribute unless no older Step writes the existing Attribute.
package process

import (
	"context"
	"errors"
	"fmt"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SlackApprovalsConnectionName is the dex-app.yaml connection of the Slack
// workspace whose channel threads the Flow follows. Studio Configuration owns it.
const SlackApprovalsConnectionName = "slack-approvals"

// The dex-app.yaml trigger bindings: a new channel message starts a Flow, and
// a reply in its thread invokes ReceiveThreadReply.
const (
	StartTriggerBinding = "slack-thread-approval-start"
	ReplyTriggerBinding = "slack-thread-approval-reply"
)

// CompletionReplyText is the message the Flow posts into the thread.
const CompletionReplyText = "Processing complete."

// postCompletionReplyStep is the Step type of the Slack reply Step.
const postCompletionReplyStep = "PostCompletionReply"

// The statuses of one thread approval.
const (
	ThreadStatusWaitingForReply = "waiting_for_reply"
	ThreadStatusPostingReply    = "posting_reply"
	ThreadStatusCompleted       = "completed"
	ThreadStatusFailed          = "failed"
	ThreadStatusUnknown         = "unknown"
)

// errThreadNotReady keeps a reply pending until RecordThread has run, so the
// reply Trigger retries it instead of consuming it.
var errThreadNotReady = errors.New("the thread approval is still recording its root message")

// ThreadInput is the Start input: the Slack thread whose root message started
// the Flow. Studio Start Flow and the start Trigger both send it.
type ThreadInput struct {
	EventID         string `json:"eventId"`
	TeamID          string `json:"teamId"`
	ChannelID       string `json:"channelId"`
	ThreadTimestamp string `json:"threadTimestamp"`
}

// ThreadReply is the input of ReceiveThreadReply, mapped from a reply event.
type ThreadReply struct {
	EventID string `json:"eventId"`
	UserID  string `json:"userId"`
}

// ThreadReplyResult tells the reply Trigger what the reply did.
type ThreadReplyResult struct {
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Status    string `json:"status"`
}

// ThreadApprovalSnapshot is the retained state of one thread approval.
type ThreadApprovalSnapshot struct {
	Status      string      `json:"status"`
	Thread      ThreadInput `json:"thread"`
	ReplyUserID string      `json:"replyUserId"`
	Notice      string      `json:"notice"`
}

var (
	// dex:indexed-attribute attribute-key:thread-status index-key:keyword1 index-type:keyword value-type:string description:"Thread approval status"
	ThreadApprovalStatus       = dex.DefineAttribute[string]("thread-status", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword1"}))
	ThreadApprovalThread       = dex.DefineAttribute[ThreadInput]("thread")
	ThreadApprovalReplyEventID = dex.DefineAttribute[string]("reply-event-id")
	ThreadApprovalReplyUserID  = dex.DefineAttribute[string]("reply-user-id")
	ThreadApprovalNotice       = dex.DefineAttribute[string]("thread-notice")
)

// SlackThreadApprovalFlow follows one Slack thread: its root message starts
// the Flow, the first matching reply posts the completion reply and ends it.
type SlackThreadApprovalFlow struct {
	dex.FlowDefaults
	Connection slack.Connection
}

// SlackThreadApprovalFlowID derives the FlowID from the Slack team, channel
// and root message timestamp, so the root and its replies reach one Flow.
func SlackThreadApprovalFlowID(teamID, channelID, threadTimestamp string) string {
	return fmt.Sprintf("slack-thread-approval-%s-%s-%s", teamID, channelID, threadTimestamp)
}

// ReadThreadApproval reads the retained snapshot; found is false for an
// unknown FlowID.
func ReadThreadApproval(ctx context.Context, client *dex.Client, flowID string) (snapshot ThreadApprovalSnapshot, found bool, err error) {
	err = client.InvokeRPC(ctx, flowID, SlackThreadApprovalFlow{}.GetThreadApproval, nil, &snapshot)
	// A read-only RPC is also served by a completed Flow, so this error means the
	// Flow never started.
	var missing *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &missing) {
		return ThreadApprovalSnapshot{}, false, nil
	}
	return snapshot, err == nil, err
}

func (flow SlackThreadApprovalFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(RecordThread{}),
		dex.DefineStep(slack.NewPostThreadReplyStep(slack.PostThreadReplyStepConfig[ThreadInput]{
			StepType:       postCompletionReplyStep,
			Connection:     flow.Connection,
			ConnectionName: SlackApprovalsConnectionName,
			Annotations:    sdkgo.StepAnnotations{GroupID: "reply", GroupLabel: "Reply", Explanation: "Post Processing complete. into the thread once a matching reply arrived."},
			MapToOperationInput: func(thread ThreadInput) slack.PostThreadReplyInput {
				return slack.PostThreadReplyInput{ChannelID: thread.ChannelID, ThreadTimestamp: thread.ThreadTimestamp, Text: CompletionReplyText}
			},
			// Failure branches never look like success: RecordReplyFailure switches on the branch.
			Sent:             sdkgo.GoTo(CompleteThread{}),
			ProviderRejected: sdkgo.GoTo(RecordReplyFailure{}), Defect: sdkgo.GoTo(RecordReplyFailure{}),
			Uncertain: sdkgo.GoTo(RecordReplyFailure{}),
		})),
		dex.DefineStep(CompleteThread{}),
		dex.DefineStep(RecordReplyFailure{}),
	}
}

func (flow SlackThreadApprovalFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.ReceiveThreadReply, &dex.RPCOptions{LockAttributes: []dex.AttributeLock{
			dex.LockAttribute(ThreadApprovalStatus), dex.LockAttribute(ThreadApprovalReplyEventID),
		}}),
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
		dex.DefineRPC(flow.GetThreadApproval, nil),
	}
}

func (SlackThreadApprovalFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{
		ThreadApprovalStatus, ThreadApprovalThread, ThreadApprovalReplyEventID, ThreadApprovalReplyUserID, ThreadApprovalNotice,
	}}
}

// GetConnectorTriggerBindings declares the two Slack Triggers; Studio
// Configuration saves each binding's channel and matchers.
func (SlackThreadApprovalFlow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		slack.DefineChannelThreadCreatedTriggerBinding(slack.ChannelThreadCreatedTriggerBindingConfig{
			ConnectionName: SlackApprovalsConnectionName, BindingName: StartTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "channel", UnitID: slack.UIUnitChannelPicker, Label: "Approval channel", Required: true,
					Description: "Select the joined Slack channel whose new messages start a thread approval.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: slack.UIChannelPickerPortChannelID, JSONPointer: "/channelId"}},
				},
				{
					ID: "message", UnitID: slack.UIUnitTextInput, Label: "Start message contains",
					Description: "Enter text the new message must contain; leave blank to match any message.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: slack.UITextInputPortText, JSONPointer: "/threadTriggerMatcher/messageContains"}},
				},
			}},
		}),
		slack.DefineThreadReplyCreatedTriggerBinding(slack.ThreadReplyCreatedTriggerBindingConfig{
			ConnectionName: SlackApprovalsConnectionName, BindingName: ReplyTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "channel", UnitID: slack.UIUnitChannelPicker, Label: "Approval channel", Required: true,
					Description: "Select the same channel as the start Trigger.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: slack.UIChannelPickerPortChannelID, JSONPointer: "/channelId"}},
				},
				{
					ID: "message", UnitID: slack.UIUnitTextInput, Label: "Reply contains",
					Description: "Enter text a reply must contain, such as approve; leave blank to match any reply.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: slack.UITextInputPortText, JSONPointer: "/threadReplyMatcher/messageContains"}},
				},
				{
					ID: "approvers", UnitID: slack.UIUnitMemberPicker, Label: "Members allowed to reply", Required: true,
					Description: "Select the members whose thread replies finish the approval.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: slack.UIMemberPickerPortMemberIDs, JSONPointer: "/threadReplyMatcher/posterUserIds"}},
				},
			}},
		}),
	}
}

// dex:group group-id:thread group-label:"Thread"
// dex:explanation text:"Keep the Slack thread that the new channel message started and wait for a reply in it."
type RecordThread struct {
	dex.StepDefaultsNoWaitFor[ThreadInput]
}

func (RecordThread) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(ThreadApprovalStatus)}}
}

func (RecordThread) Execute(ctx dex.Context, thread ThreadInput) (_ *dex.StepDecision, err error) {
	defer logFailure(ctx, SlackThreadApprovalFlow{}, "RecordThread", &err)
	if err = errors.Join(ThreadApprovalThread.Set(ctx, thread), ThreadApprovalStatus.Set(ctx, ThreadStatusWaitingForReply)); err != nil {
		return nil, err
	}
	return dex.DeadEnd(), nil
}

// dex:group group-id:reply group-label:"Reply"
// dex:explanation text:"Complete once Slack confirms the completion reply."
type CompleteThread struct {
	dex.StepDefaultsNoWaitFor[slack.PostThreadReplyResult]
}

func (CompleteThread) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(ThreadApprovalStatus)}}
}

func (CompleteThread) Execute(ctx dex.Context, _ slack.PostThreadReplyResult) (_ *dex.StepDecision, err error) {
	defer logFailure(ctx, SlackThreadApprovalFlow{}, "CompleteThread", &err)
	if err = ThreadApprovalStatus.Set(ctx, ThreadStatusCompleted); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(nil), nil
}

// dex:group group-id:reply group-label:"Reply"
// dex:explanation text:"Record why the completion reply was not confirmed, without posting it again."
type RecordReplyFailure struct {
	dex.StepDefaultsNoWaitFor[slack.PostThreadReplyResult]
}

func (RecordReplyFailure) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteLockAttributes: []dex.AttributeLock{dex.LockAttribute(ThreadApprovalStatus)}}
}

// Execute receives the rejected, defect and uncertain branches.
func (RecordReplyFailure) Execute(ctx dex.Context, result slack.PostThreadReplyResult) (_ *dex.StepDecision, err error) {
	defer logFailure(ctx, SlackThreadApprovalFlow{}, "RecordReplyFailure", &err)
	status, notice := ThreadStatusFailed, "Slack did not accept the completion reply. Check the slack-approvals connection in Studio Configuration."
	if result.Branch == slack.PostThreadReplyBranchUncertain {
		status, notice = ThreadStatusUnknown, "Slack did not confirm the completion reply; it may have been posted. Check the thread."
	}
	if err = errors.Join(ThreadApprovalStatus.Set(ctx, status), ThreadApprovalNotice.Set(ctx, notice)); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(nil), nil
}

// ReceiveThreadReply accepts the first matching reply while the Flow waits; it
// rechecks the status under its lock, so a repeated reply posts nothing again.
func (SlackThreadApprovalFlow) ReceiveThreadReply(ctx dex.Context, reply ThreadReply) (_ *dex.RPCResult[ThreadReplyResult], err error) {
	defer logFailure(ctx, SlackThreadApprovalFlow{}, "ReceiveThreadReply", &err)
	status, err := optional(ctx, ThreadApprovalStatus)
	if err != nil {
		return nil, err
	}
	acceptedEventID, err := optional(ctx, ThreadApprovalReplyEventID)
	if err != nil {
		return nil, err
	}
	switch {
	case acceptedEventID != "" && acceptedEventID == reply.EventID:
		return &dex.RPCResult[ThreadReplyResult]{Output: ThreadReplyResult{Duplicate: true, Status: status}}, nil
	case status == "":
		return nil, errThreadNotReady
	case status != ThreadStatusWaitingForReply:
		return &dex.RPCResult[ThreadReplyResult]{Output: ThreadReplyResult{Status: status}}, nil
	}
	thread, err := ThreadApprovalThread.Get(ctx)
	if err != nil {
		return nil, err
	}
	err = errors.Join(
		ThreadApprovalStatus.Set(ctx, ThreadStatusPostingReply),
		ThreadApprovalReplyEventID.Set(ctx, reply.EventID),
		ThreadApprovalReplyUserID.Set(ctx, reply.UserID),
	)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[ThreadReplyResult]{
		Output:    ThreadReplyResult{Accepted: true, Status: ThreadStatusPostingReply},
		NextSteps: []dex.StepMovement{dex.MovementOf(sdkgo.StepRef[ThreadInput](postCompletionReplyStep), thread)},
	}, nil
}

// dex:field attribute-key:reply-user-id value-type:string editable:false description:"Replied by"
// dex:field attribute-key:thread-notice value-type:string editable:false description:"Notice"
func (SlackThreadApprovalFlow) GetDexSummary(ctx dex.Context, _ dex.None) (_ *dex.RPCResult[map[string]any], err error) {
	defer logFailure(ctx, SlackThreadApprovalFlow{}, "GetDexSummary", &err)
	snapshot, err := readThreadApproval(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"reply-user-id": snapshot.ReplyUserID, "thread-notice": snapshot.Notice,
	}}, nil
}

// dex:field attribute-key:thread-status value-type:string editable:false ui-slot:status description:"Status"
// dex:field attribute-key:thread value-type:json editable:false description:"Slack thread"
// dex:field attribute-key:reply-user-id value-type:string editable:false description:"Replied by"
// dex:field attribute-key:thread-notice value-type:string editable:false ui-slot:reason description:"Notice"
func (SlackThreadApprovalFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (_ *dex.RPCResult[map[string]any], err error) {
	defer logFailure(ctx, SlackThreadApprovalFlow{}, "GetDexDisplay", &err)
	snapshot, err := readThreadApproval(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"thread-status": snapshot.Status, "thread": snapshot.Thread,
		"reply-user-id": snapshot.ReplyUserID, "thread-notice": snapshot.Notice,
	}}, nil
}

// GetThreadApproval is read-only, so it also serves a completed Flow.
func (SlackThreadApprovalFlow) GetThreadApproval(ctx dex.Context, _ dex.None) (_ *dex.RPCResult[ThreadApprovalSnapshot], err error) {
	defer logFailure(ctx, SlackThreadApprovalFlow{}, "GetThreadApproval", &err)
	snapshot, err := readThreadApproval(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[ThreadApprovalSnapshot]{Output: snapshot}, nil
}

// readThreadApproval reads every Attribute; status is "pending" until RecordThread runs.
func readThreadApproval(ctx dex.Context) (snapshot ThreadApprovalSnapshot, err error) {
	var errs [4]error
	snapshot.Status, errs[0] = optional(ctx, ThreadApprovalStatus)
	snapshot.Thread, errs[1] = optional(ctx, ThreadApprovalThread)
	snapshot.ReplyUserID, errs[2] = optional(ctx, ThreadApprovalReplyUserID)
	snapshot.Notice, errs[3] = optional(ctx, ThreadApprovalNotice)
	if snapshot.Status == "" {
		snapshot.Status = "pending"
	}
	return snapshot, errors.Join(errs[:]...)
}

// optional returns the zero value for an Attribute the Flow has not written yet.
func optional[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return value, nil
	}
	return value, err
}
