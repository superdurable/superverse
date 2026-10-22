// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/attachments"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// agentServer serves AgentService: the commands of a Session's Agent. A
// command's empty response confirms its admission; the Session Watch owns the
// state that follows.
type agentServer struct {
	pb.UnimplementedAgentServiceServer
	*boundary.Boundary
	sessions      *agents.SessionFlowService
	sessionAgents *sessionagent.SessionAgentFlowClient
	attachments   *attachments.AttachmentFlowService
	models        SessionModels
}

// SessionModels is which Session models the server offers and to whom.
type SessionModels struct {
	// Claude offers the Claude catalog; each Session admits its own credential.
	Claude bool
	// Mock offers the mock model (agent.mockModel).
	Mock bool
	// Local is Local Kind and Native, where every account may select the mock
	// model; elsewhere only the administrator may.
	Local bool
}

// commandRefusalError is the public refusal of an Agent command that its
// admission decides after the caller and the Session are authorized, such as
// a model the caller may not select.
type commandRefusalError struct{ public error }

func (refusal *commandRefusalError) Error() string { return refusal.public.Error() }

// agentCommandRequest is the Project whose Session every Agent command names.
type agentCommandRequest interface {
	GetCaller() *pb.CallerContext
	GetProjectId() string
}

// admitCommand admits one command to the caller's Session Agent and returns
// the command's empty response R. The Session is authorized before invalid,
// the command's input check, is reported, so an unauthorized caller learns
// nothing about its input. A failure before dispatch and a definite refusal
// carry AGENT_COMMAND_NOT_ADMITTED, which tells Studio that the command had no
// effect. A Session whose engine execution stopped refuses every command, also
// one that raced the end of the execution, with FAILED_PRECONDITION and the
// reason SESSION_EXECUTION_FAILED, _CANCELED or _TERMINATED, which Studio
// presents as a FAILED Session.
func admitCommand[R any](ctx context.Context, s *agentServer, in agentCommandRequest, invalid error, admit func(agents.SessionCaller) error) (*R, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, boundary.NotAdmitted(err)
	}
	id, err := s.sessions.AuthorizeSession(ctx, account, in.GetProjectId())
	if err != nil {
		return nil, notAdmittedCommand(err)
	}
	if invalid != nil {
		return nil, boundary.NotAdmitted(invalid)
	}
	err = admit(id)
	if errors.Is(err, modelcredential.ErrUnavailable) {
		return nil, boundary.NotAdmitted(status.Error(codes.FailedPrecondition, "set your key or connect your local agent"))
	}
	if errors.Is(err, sessionagent.ErrQueuedMessageNotAuthor) {
		return nil, boundary.NotAdmitted(status.Error(codes.PermissionDenied, "only the author or an owner may delete a queued message"))
	}
	if refusal, isRefused := errors.AsType[*commandRefusalError](err); isRefused {
		return nil, boundary.NotAdmitted(refusal.public)
	}
	if errors.Is(err, sessionagent.ErrAttachmentsNeedPlatformAgent) {
		return nil, boundary.NotAdmitted(status.Error(codes.FailedPrecondition, "attachments work with the Platform agent"))
	}
	if _, isStopped := errors.AsType[*sessionagent.SessionExecutionStoppedError](err); isStopped || errors.Is(err, sessionagent.ErrCommandRejected) {
		return nil, notAdmittedCommand(err)
	}
	if err != nil {
		return nil, boundary.AgentRPCStatus(err)
	}
	return new(R), nil
}

// notAdmittedCommand is the public status of a command that definitely had no
// effect. A stopped engine execution keeps its own reason.
func notAdmittedCommand(err error) error {
	if _, isStopped := errors.AsType[*sessionagent.SessionExecutionStoppedError](err); isStopped {
		return boundary.AgentRPCStatus(err)
	}
	return boundary.NotAdmitted(boundary.AgentRPCStatus(err))
}

// SendMessage queues a person's message, with its attachments: each READY,
// the caller's own upload and of the Project's TeamSpace, which the Project
// counts under this call's own admission before the Session admits the
// message (AttachmentFlowService). After a definite refusal of the Session
// the admission's holds are dropped, which frees only what no other
// admission, of another message or another send of this one, holds; after
// an uncertain result they stay.
func (s *agentServer) SendMessage(ctx context.Context, in *pb.SendMessageRequest) (*pb.SendMessageResponse, error) {
	ids := in.GetAttachmentIds()
	invalid := boundary.ValidateMessageID(in.GetMessageId())
	if invalid == nil {
		invalid = validAttachmentIDs(ids, s.attachments)
	}
	if invalid == nil {
		invalid = boundary.InvalidIf(!validMessageContent(in.GetText(), len(ids) > 0), "invalid message content")
	}
	message := conversation.PendingUserMessage{MessageID: conversation.MessageID(in.GetMessageId()), Value: conversation.UserMessage{Content: in.GetText()}}
	return admitCommand[pb.SendMessageResponse](ctx, s, in, invalid, func(caller agents.SessionCaller) error {
		message.Value.Actor = &caller.Actor
		if len(ids) == 0 {
			return s.sessionAgents.SendPendingMessage(ctx, caller.FlowID, message)
		}
		projectID := in.GetProjectId()
		admission, err := s.attachments.AdmitMessage(ctx, caller.Actor.AccountID, projectID, caller.TeamSpaceID, in.GetMessageId(), ids)
		if err != nil {
			return attachmentRefusal(err)
		}
		message.Value.Attachments = admission.References
		err = s.sessionAgents.SendPendingMessage(ctx, caller.FlowID, message)
		if refusedSend(err) {
			if released := s.attachments.ReleaseMessage(context.WithoutCancel(ctx), projectID, admission); released != nil {
				// The refusal stands; the attachments stay counted, as after an uncertain result.
				s.Logger.WarnContext(ctx, "Attachment reservation not released", "project", projectID, "message", in.GetMessageId(), "error", released)
			}
		}
		return err
	})
}

// refusedSend reports whether the Session definitely refused a message: a
// question waits, a local agent executes it, it is closed or its engine
// execution stopped. Any other error leaves the message's admission
// uncertain.
func refusedSend(err error) bool {
	_, stopped := errors.AsType[*sessionagent.SessionExecutionStoppedError](err)
	return stopped || errors.Is(err, sessionagent.ErrCommandRejected) || errors.Is(err, sessionagent.ErrAttachmentsNeedPlatformAgent)
}

// attachmentRefusal is the public refusal of a message for its attachments:
// an AttachmentFlowService refusal or a Project that admits none. Any other
// error is the admission's own.
func attachmentRefusal(err error) error {
	if refusal, isRefused := errors.AsType[*attachments.AdmissionRefusal](err); isRefused {
		return &commandRefusalError{attachmentRefusalStatus(refusal)}
	}
	if _, isRejected := errors.AsType[*dexflow.Rejection](err); isRejected {
		return &commandRefusalError{boundary.RPCStatus(err)}
	}
	return err
}

// attachmentRefusalStatus is the status of an admission refusal, whose
// message starts with its code.
func attachmentRefusalStatus(refusal *attachments.AdmissionRefusal) error {
	code := codes.FailedPrecondition
	switch refusal.Code {
	case attachments.RefusalNotFound:
		code = codes.NotFound
	case attachments.RefusalNotYours:
		code = codes.PermissionDenied
	case attachments.RefusalQuotaExceeded:
		code = codes.ResourceExhausted
	case attachments.RefusalNotReady, attachments.RefusalExpired:
	}
	return status.Error(code, refusal.Error())
}

// validAttachmentIDs admits the distinct attachment IDs of a message, at most
// what admission admits; a message without attachments needs no admission.
func validAttachmentIDs(ids []string, admission *attachments.AttachmentFlowService) error {
	if len(ids) == 0 {
		return nil
	}
	if maximum := admission.MaxPerMessage(); len(ids) > maximum {
		return status.Errorf(codes.InvalidArgument, "a message carries at most %d attachments", maximum)
	}
	for index, id := range ids {
		if err := flowid.ValidateAttachmentID(id); err != nil {
			return boundary.RPCStatus(err)
		}
		if slices.Contains(ids[:index], id) {
			return status.Error(codes.InvalidArgument, "an attachment is named twice")
		}
	}
	return nil
}

func (s *agentServer) AnswerQuestions(ctx context.Context, in *pb.AnswerQuestionsRequest) (*pb.AnswerQuestionsResponse, error) {
	request, invalid := answerQuestionsRequest(in.GetInput())
	return admitCommand[pb.AnswerQuestionsResponse](ctx, s, in, invalid, func(caller agents.SessionCaller) error {
		request.Actor = &caller.Actor
		return s.sessionAgents.AnswerQuestions(ctx, caller.FlowID, request)
	})
}

func (s *agentServer) UpdateSessionModel(ctx context.Context, in *pb.UpdateSessionModelRequest) (*pb.UpdateSessionModelResponse, error) {
	model, isKnown := sessionModels[in.GetModel()]
	// The largest revision is rejected so that the Agent's acknowledgement
	// check of ExpectedRevision+1 cannot wrap to zero.
	invalid := boundary.InvalidIf(!isKnown || in.ExpectedRevision == nil || in.GetExpectedRevision() == math.MaxUint64, "a supported model and the observed model revision are required")
	request := agentloop.UpdateSessionModelRequest{Model: model, ExpectedRevision: in.GetExpectedRevision()}
	return admitCommand[pb.UpdateSessionModelResponse](ctx, s, in, invalid, func(caller agents.SessionCaller) error {
		// admitCommand authenticated the caller's account ID.
		if err := admitModel(ctx, s.Boundary, s.models, in.GetCaller().GetAccountId(), model); err != nil {
			return err
		}
		request.Actor = &caller.Actor
		return s.sessionAgents.UpdateSessionModel(ctx, caller.FlowID, request)
	})
}

// admitModel decides whether the account may select model here, for
// UpdateSessionModel and CreateProject. Claude is a catalog capability; the
// Session checks its own credential. The mock model needs an offer and, outside Local
// Kind and Native, the administrator.
func admitModel(ctx context.Context, b *boundary.Boundary, models SessionModels, accountID string, model conversation.Model) error {
	if availability := modelprovider.ProfileOf(model).Availability; !availability.Selectable() {
		return &commandRefusalError{status.Error(codes.FailedPrecondition, availability.Reason())}
	}
	if model != conversation.ModelMock {
		if !models.Claude {
			return &commandRefusalError{status.Error(codes.FailedPrecondition, "this environment does not offer Claude models")}
		}
		return nil
	}
	if !models.Mock {
		return &commandRefusalError{status.Error(codes.FailedPrecondition, "this environment does not offer the mock model")}
	}
	selectable, err := mockSelectable(ctx, b, models, accountID)
	if err != nil {
		// The command did not run, so a failed read of the administrator refuses it.
		return &commandRefusalError{boundary.RPCStatus(err)}
	}
	if !selectable {
		return &commandRefusalError{status.Error(codes.PermissionDenied, "only the administrator may select the mock model in this environment")}
	}
	return nil
}

// mockSelectable reports whether the account may select the mock model: the
// server offers it, and the environment is Local or the account is the
// administrator. The authority is the account itself, never a browser role.
func mockSelectable(ctx context.Context, b *boundary.Boundary, models SessionModels, accountID string) (bool, error) {
	if !models.Mock || models.Local {
		return models.Mock, nil
	}
	return b.Accounts.IsAdministrator(ctx, accountID)
}

// ListMockScenarios returns the mock model's scenarios when the caller may
// select it, so Studio offers the mock model and its inputs only then.
func (s *agentServer) ListMockScenarios(ctx context.Context, in *pb.ListMockScenariosRequest) (*pb.ListMockScenariosResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	selectable, err := mockSelectable(ctx, s.Boundary, s.models, account.AccountID)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListMockScenariosResponse{Selectable: selectable}
	if !selectable {
		return out, nil
	}
	for _, scenario := range mockmodel.Catalog() {
		item := &pb.AgentMockScenario{Name: scenario.Name, Title: scenario.Title, Description: scenario.Description,
			Category: boundary.Enum[pb.AgentMockScenario_Category](scenario.Category.String()), NeedsNewProject: scenario.NeedsNewProject, TestAction: scenario.TestAction}
		for _, argument := range scenario.Arguments {
			item.Arguments = append(item.Arguments, &pb.AgentMockScenarioArgument{Name: argument.Name, DefaultValue: argument.Default})
		}
		for _, input := range scenario.Inputs {
			item.Inputs = append(item.Inputs, &pb.AgentMockScenarioInput{Number: int32(input.Number), Label: input.Label, //nolint:gosec // a scenario has a few inputs
				Kind: boundary.Enum[pb.AgentMockScenarioInput_Kind](input.Kind), Content: input.Content, Answers: input.Answers})
		}
		out.Scenarios = append(out.Scenarios, item)
	}
	return out, nil
}

// GetFlowDesign reads the Flow design of the Session's current requirements,
// at the digest the caller read from the Snapshot.
func (s *agentServer) GetFlowDesign(ctx context.Context, in *pb.GetFlowDesignRequest) (*pb.GetFlowDesignResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	slot, isKnown := flowDesignSlots[in.GetSlot()]
	if err = boundary.InvalidIf(!isKnown || in.GetDigest() == "", "a design slot and the digest of its reference are required"); err != nil {
		return nil, err
	}
	id, err := s.sessions.AuthorizeSession(ctx, account, in.GetProjectId())
	if err != nil {
		return nil, boundary.AgentRPCStatus(err)
	}
	design, err := s.sessionAgents.GetFlowDesign(ctx, id.FlowID, slot, in.GetDigest())
	if err != nil {
		return nil, boundary.AgentRPCStatus(err)
	}
	return &pb.GetFlowDesignResponse{Design: agentFlowDesign(design, in.GetDigest())}, nil
}

func (s *agentServer) DeleteQueuedMessage(ctx context.Context, in *pb.DeleteQueuedMessageRequest) (*pb.DeleteQueuedMessageResponse, error) {
	return admitCommand[pb.DeleteQueuedMessageResponse](ctx, s, in, boundary.ValidateMessageID(in.GetMessageId()), func(caller agents.SessionCaller) error {
		return s.sessionAgents.DeleteQueuedMessage(ctx, caller.FlowID, conversation.DeleteQueuedMessageRequest{MessageID: conversation.MessageID(in.GetMessageId()), Actor: caller.Actor, IsOwner: caller.Role == projects.RoleOwner})
	})
}

func (s *agentServer) SteerQueuedMessage(ctx context.Context, in *pb.SteerQueuedMessageRequest) (*pb.SteerQueuedMessageResponse, error) {
	return admitCommand[pb.SteerQueuedMessageResponse](ctx, s, in, boundary.ValidateMessageID(in.GetMessageId()), func(caller agents.SessionCaller) error {
		return s.sessionAgents.SteerMessage(ctx, caller.FlowID, conversation.SteerMessageRequest{Actor: &caller.Actor, MessageID: conversation.MessageID(in.GetMessageId())})
	})
}

// ResolveToolApproval approves or denies the pending tool call.
func (s *agentServer) ResolveToolApproval(ctx context.Context, in *pb.ResolveToolApprovalRequest) (*pb.ResolveToolApprovalResponse, error) {
	invalid := boundary.InvalidIf(strings.TrimSpace(in.GetCallId()) == "" || in.IsApproved == nil, "call ID and explicit approval decision are required")
	return admitCommand[pb.ResolveToolApprovalResponse](ctx, s, in, invalid, func(caller agents.SessionCaller) error {
		return s.sessionAgents.ApproveTool(ctx, caller.FlowID, agentloop.ToolApprovalRequest{CallID: conversation.CallID(in.GetCallId()), Approved: in.GetIsApproved()})
	})
}

// InterruptAgent is the Stop button for the turn the user saw. A Stop of an
// earlier turn, or with nothing running, is not admitted.
func (s *agentServer) InterruptAgent(ctx context.Context, in *pb.InterruptAgentRequest) (*pb.InterruptAgentResponse, error) {
	invalid := boundary.InvalidIf(in.GetTurn() < 0, "the turn is not negative")
	return admitCommand[pb.InterruptAgentResponse](ctx, s, in, invalid, func(caller agents.SessionCaller) error {
		return s.sessionAgents.InterruptAgent(ctx, caller.FlowID, agentloop.InterruptAgentRequest{Actor: &caller.Actor, Turn: in.GetTurn()})
	})
}

// answerQuestionsRequest maps the answers to a question batch: one to three,
// one per question, which the pending batch checks.
func answerQuestionsRequest(in *pb.AgentAnswerQuestionsInput) (agentloop.AnswerQuestionsRequest, error) {
	if strings.TrimSpace(in.GetCallId()) == "" || len(in.GetAnswers()) == 0 || len(in.GetAnswers()) > 3 {
		return agentloop.AnswerQuestionsRequest{}, status.Error(codes.InvalidArgument, "call ID and one to three answers are required")
	}
	out := agentloop.AnswerQuestionsRequest{CallID: conversation.CallID(in.GetCallId())}
	seen := map[string]bool{}
	for _, answer := range in.GetAnswers() {
		if strings.TrimSpace(answer.GetQuestionId()) == "" || strings.TrimSpace(answer.GetAnswer()) == "" || seen[answer.GetQuestionId()] {
			return agentloop.AnswerQuestionsRequest{}, status.Error(codes.InvalidArgument, "answers require distinct question IDs and nonempty answers")
		}
		seen[answer.GetQuestionId()] = true
		out.Answers = append(out.Answers, agentloop.UserInputAnswer{QuestionID: agentloop.UserInputQuestionID(answer.GetQuestionId()), Answer: answer.GetAnswer()})
	}
	return out, nil
}

// flowDesignSlots maps the API design slot enum to the Session's slots.
var flowDesignSlots = map[pb.AgentFlowDesignSlot]flowdesign.Slot{
	pb.AgentFlowDesignSlot_AGENT_FLOW_DESIGN_SLOT_CONFIRMED: flowdesign.SlotConfirmed,
}

// sessionModels maps the API model enum to the Session's models.
var sessionModels = map[pb.AgentModel]conversation.Model{
	pb.AgentModel_AGENT_MODEL_GPT_6_ASTRA:        conversation.ModelGPT6Astra,
	pb.AgentModel_AGENT_MODEL_DEEPSEEK_FLASH:     conversation.ModelDeepSeekFlash,
	pb.AgentModel_AGENT_MODEL_GROK_4_7:           conversation.ModelGrok47,
	pb.AgentModel_AGENT_MODEL_MUSE_WONDER_1_0:    conversation.ModelMuseWonder10,
	pb.AgentModel_AGENT_MODEL_GEMINI_4_ARGON:     conversation.ModelGemini4Argon,
	pb.AgentModel_AGENT_MODEL_KIMI_K3:            conversation.ModelKimiK3,
	pb.AgentModel_AGENT_MODEL_GLM_5_3:            conversation.ModelGLM53,
	pb.AgentModel_AGENT_MODEL_QWEN_3_8_MAX:       conversation.ModelQwen38Max,
	pb.AgentModel_AGENT_MODEL_MINIMAX_M3_1_FLASH: conversation.ModelMiniMaxM31Flash,
	pb.AgentModel_AGENT_MODEL_KIMI_K2_6:          conversation.ModelKimiK26,
	pb.AgentModel_AGENT_MODEL_GLM_5_2:            conversation.ModelGLM52,
	pb.AgentModel_AGENT_MODEL_MINIMAX_M3:         conversation.ModelMiniMaxM3,
	pb.AgentModel_AGENT_MODEL_OPUS_5_5:           conversation.ModelOpus55,
	pb.AgentModel_AGENT_MODEL_SONNET_5_5:         conversation.ModelSonnet55,
	pb.AgentModel_AGENT_MODEL_HAIKU_4_5:          conversation.ModelHaiku45,
	pb.AgentModel_AGENT_MODEL_MOCK:               conversation.ModelMock,
}

// agentModel is the API enum of a Session model; unspecified for none.
func agentModel(model conversation.Model) pb.AgentModel {
	if model == "" {
		return pb.AgentModel_AGENT_MODEL_UNSPECIFIED
	}
	for value, sessionModel := range sessionModels {
		if sessionModel == model {
			return value
		}
	}
	panic("catalog model has no AgentModel mapping")
}

// validMessageContent is the content rule of every new user message: valid
// UTF-8 without NUL, within the content bound, and nonblank unless the
// message carries attachments.
func validMessageContent(text string, attached bool) bool {
	return (attached || strings.TrimSpace(text) != "") && !strings.ContainsRune(text, '\x00') && utf8.ValidString(text) && len(text) <= conversation.MaximumUserMessageContentBytes
}

// Agent conversion belongs to the API boundary; the durable Agent has no
// protobuf dependency. Every Agent enum validates itself when it is decoded
// from a Flow result or Stream, so boundary.Enum meets only declared values.

// agentSnapshot preserves the complete safe conversation view without provider context.
func agentSnapshot(value agentloop.AgentSnapshot) *pb.AgentSnapshot {
	return &pb.AgentSnapshot{History: agentHistoryPage(value.History), Description: agentDescription(value.Description), Queued: agentPendingMessages(value.Queued), Steered: agentPendingMessages(value.Steered)}
}

// agentHistoryPage maps one ascending page of retained history, which the
// Snapshot and the archive reads both return.
func agentHistoryPage(page conversation.HistoryPage) *pb.AgentHistoryPage {
	history := &pb.AgentHistoryPage{NextBeforeSequence: agentInt64Pointer(page.NextBeforeSequence)}
	for _, message := range page.Messages {
		history.Messages = append(history.Messages, agentMessage(message))
	}
	return history
}

// agentMessage maps the public view of one entry with its timestamps and optional message identities.
func agentMessage(value conversation.SequencedMessage) *pb.AgentSequencedMessage {
	m := value.Message
	message := &pb.AgentMessage{Actor: agentActor(m.Actor), SteeredBy: agentActor(m.SteeredBy), Via: m.Via, Imported: m.Imported, Role: boundary.Enum[pb.AgentMessage_Role](m.Role), Content: m.Content, ToolCallId: agentStringPointer(m.ToolCallID), ToolName: agentStringPointer(m.ToolName), AnsweredInputCallId: agentStringPointer(m.AnsweredInputCallID), CreatedAt: m.CreatedAt.Format(time.RFC3339Nano), StartedAt: agentDatePointer(m.StartedAt)}
	if metrics := m.ModelCall; metrics != nil {
		message.ModelCall = &pb.AgentModelCallMetrics{Model: metrics.Model, ServedModel: metrics.ServedModel, Effort: string(metrics.Effort),
			DurationMs: metrics.DurationMS, FirstEventMs: metrics.FirstEventMS, InputTokens: metrics.InputTokens,
			CacheReadInputTokens: metrics.CacheReadInputTokens, CacheCreationInputTokens: metrics.CacheCreationInputTokens,
			OutputTokens: metrics.OutputTokens, ThinkingTokens: metrics.ThinkingTokens, DroppedThinkingBlocks: int64(metrics.DroppedThinkingBlocks)}
	}
	if metrics := m.ToolExecution; metrics != nil {
		message.ToolExecution = &pb.AgentToolExecutionMetrics{Outcome: boundary.Enum[pb.AgentToolExecutionMetrics_Outcome](metrics.Outcome),
			DurationMs: metrics.DurationMS, ResultBytes: metrics.ResultBytes, OmittedBytes: metrics.OmittedBytes, ResultCode: metrics.ResultCode}
		if metrics.ExitCode != nil {
			code := int32(*metrics.ExitCode) //nolint:gosec // a process exit status fits in 32 bits
			message.ToolExecution.ExitCode = &code
		}
	}
	for _, call := range m.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, &pb.AgentToolCall{Id: string(call.ID), Name: string(call.Name), Target: call.Target})
	}
	message.Attachments = agentAttachments(m.Attachments)
	if n := m.Notification; n != nil {
		message.Notification = &pb.AgentNotification{Id: n.ID, Kind: boundary.Enum[pb.AgentNotification_Kind](n.Kind), Source: boundary.Enum[pb.AgentNotification_Source](n.Source),
			OccurredAt: n.OccurredAt.UTC().Format(time.RFC3339Nano), TaskId: n.TaskID, TaskStatus: boundary.Enum[pb.AgentNotification_TaskStatus](n.TaskStatus), Summary: n.Summary,
			Outcome: boundary.Enum[pb.AgentNotification_Outcome](n.Outcome.String()), Owner: boundary.Enum[pb.AgentNotification_Owner](n.Owner.String()), Code: n.Code, Phase: n.Phase,
			Commit: n.Commit, ResourceId: n.ResourceID, ResolvedNoticeId: n.ResolvedNoticeID, Url: n.URL, Gaps: n.Gaps, GapCount: int32(n.GapCount), Message: n.Message, //nolint:gosec // a notification names a bounded number of gaps
			Remedy: boundary.Enum[pb.FailureRemedy](n.Remedy.String()),
			Slug:   n.Slug, ConnectorEvent: boundary.Enum[pb.AgentNotification_ConnectorEvent](n.Event.String()), Version: boundary.CommitVersionSnapshot(n.Version)}
	}
	return &pb.AgentSequencedMessage{Sequence: int64(value.Sequence), Message: message}
}

// agentDescription maps all durable pending states and the progress list.
func agentDescription(value agentloop.AgentDescription) *pb.AgentDescription {
	out := &pb.AgentDescription{ModelCredentialVersion: value.ModelCredentialVersion, Executor: sessionExecutor(value.Executor), Status: boundary.Enum[pb.AgentDescription_Status](value.Status), WaitingInputRound: int64(value.WaitingInputRound), Model: agentModel(value.Model), ModelRevision: value.ModelRevision, ContextModel: agentModel(value.ContextModel), Effort: string(value.Effort), FirstRetainedSequence: int64(value.FirstRetainedSequence), LastSequence: int64(value.LastSequence), PendingQueuedMessageCount: int64(value.PendingQueuedMessageCount), PendingSteeredMessageCount: int64(value.PendingSteeredMessageCount), Turn: value.Turn,
		IsPreviewAutoDeployEnabled: value.IsPreviewAutoDeployEnabled, ModelChangedBy: agentActor(value.ModelChangedBy), PreviewAutoDeployChangedBy: agentActor(value.PreviewAutoDeployChangedBy)}
	for _, tool := range value.AvailableTools {
		out.AvailableTools = append(out.AvailableTools, string(tool))
	}
	for _, command := range value.BackgroundCommands {
		out.BackgroundCommands = append(out.BackgroundCommands, agentBackgroundCommand(command))
	}
	for _, record := range value.Subagents {
		out.Subagents = append(out.Subagents, agentSubagent(record))
	}
	if p := value.StartingExample; p != nil {
		out.StartingExample = &pb.AgentStartingExample{ExampleId: p.ExampleID, Choice: boundary.Enum[pb.AgentStartingExample_Choice](p.Choice)}
		if build := p.FirstBuild; build != nil {
			out.StartingExample.FirstBuild = &pb.AgentStartingExampleBuild{ExampleSteps: int32(build.ExampleSteps), BuiltSteps: int32(build.BuiltSteps), KeptSteps: int32(build.KeptSteps), Commit: build.Commit} //nolint:gosec // counts bounded by the catalog and the gate's 256-Step limit
		}
	}
	if p := value.MockProgress; p != nil {
		out.MockProgress = &pb.AgentMockProgress{Scenario: p.Scenario, Input: int32(p.Input), NextInput: int32(p.NextInput)} //nolint:gosec // a scenario has a few inputs
	}
	if p := value.PendingApproval; p != nil {
		out.PendingApproval = &pb.AgentPendingApproval{CallId: string(p.CallID), ToolName: string(p.ToolName), ArgumentsJson: p.Arguments.String(), StartedAt: agentDatePointer(p.StartedAt)}
	}
	if p := value.PendingTimer; p != nil {
		out.PendingTimer = &pb.AgentPendingTimer{CallId: string(p.CallID), DurationSeconds: p.DurationSeconds, Reason: p.Reason, StartedAt: agentDatePointer(p.StartedAt)}
	}
	if p := value.PendingUserInput; p != nil {
		out.PendingUserInput = &pb.AgentPendingUserInput{CallId: string(p.CallID), StartedAt: agentDatePointer(p.StartedAt), Kind: pendingUserInputKind(p.Kind), Requirements: agentRequirements(p.Requirements)}
		for _, question := range p.Questions {
			q := &pb.AgentUserInputQuestion{Id: string(question.ID), Header: question.Header, Question: question.Question}
			for _, option := range question.Options {
				q.Options = append(q.Options, &pb.AgentUserInputOption{Label: option.Label, Description: option.Description})
			}
			out.PendingUserInput.Questions = append(out.PendingUserInput.Questions, q)
		}
	}
	if p := value.ConfirmedRequirements; p != nil {
		out.ConfirmedRequirements = &pb.AgentConfirmedRequirements{Revision: p.Revision, Spec: agentRequirements(&p.Spec), ConfirmedAt: p.ConfirmedAt.UTC().Format(time.RFC3339Nano), ConfirmedBy: agentActor(p.ConfirmedBy)}
		out.ConfirmedRequirements.Spec.DesignBuilt = value.ConfirmedDesignBuilt
		out.ConfirmedRequirements.Spec.DesignBuiltCommit = value.ConfirmedDesignBuiltCommit
		out.ConfirmedRequirements.Spec.DesignWrittenSteps = value.ConfirmedDesignWrittenSteps
	}
	if p := value.TodoList; p != nil {
		out.TodoList = &pb.AgentTodoList{Revision: p.Revision}
		for _, item := range p.Items {
			out.TodoList.Items = append(out.TodoList.Items, &pb.AgentTodoItem{Content: item.Content, Status: boundary.Enum[pb.AgentTodoItem_Status](item.Status)})
		}
	}
	return out
}

// agentBackgroundCommand maps the task record of one background command; it
// has no command text or output.
func agentBackgroundCommand(value agenttasks.BackgroundCommand) *pb.AgentBackgroundCommand {
	out := &pb.AgentBackgroundCommand{TaskId: value.TaskID, Description: value.Description, Status: boundary.Enum[pb.AgentBackgroundCommand_Status](value.Status),
		EndReason: boundary.Enum[pb.AgentBackgroundCommand_EndReason](value.EndReason), OutputBytes: value.OutputBytes,
		StartedAt: value.StartedAt.UTC().Format(time.RFC3339Nano), EndedAt: agentDatePointer(&value.EndedAt)}
	if value.ExitCode != nil {
		code := int32(*value.ExitCode) //nolint:gosec // a process exit status fits in 32 bits
		out.ExitCode = &code
	}
	return out
}

// agentSubagent maps the Session's record of one subagent; it has no prompt
// and no report. A record that counts as lost now is marked so, which takes it
// out of the running view as the Session's own limits and Stop do.
func agentSubagent(value agenttasks.SubagentRecord) *pb.AgentSubagent {
	out := &pb.AgentSubagent{TaskId: value.TaskID(), Kind: boundary.Enum[pb.AgentSubagent_Kind](value.Kind.String()), Description: value.Description,
		Status: boundary.Enum[pb.AgentSubagent_Status](value.Status.String()), StartedAt: value.StartedAt.UTC().Format(time.RFC3339Nano),
		FinishedAt: agentDatePointer(&value.FinishedAt), Summary: value.Summary, Lost: value.IsLost(time.Now())}
	if changes := value.Changes; changes != nil {
		//nolint:gosec // the changes of one bounded task fit in 32 bits
		out.ChangedFiles, out.Insertions, out.Deletions = int32(changes.Files), int32(changes.Insertions), int32(changes.Deletions)
	}
	return out
}

// pendingUserInputKind is the wire kind of a pending input. The domain value
// of a question batch is empty, which has its own wire value.
func pendingUserInputKind(kind agentloop.PendingUserInputKind) pb.AgentPendingUserInput_Kind {
	if kind.CurrentKind() == "" {
		return pb.AgentPendingUserInput_KIND_QUESTION
	}
	return boundary.Enum[pb.AgentPendingUserInput_Kind](kind)
}

// agentRequirements maps the requirements of a card or a recorded version; nil
// stays nil.
func agentRequirements(value *agentloop.Requirements) *pb.AgentRequirements {
	if value == nil {
		return nil
	}
	out := &pb.AgentRequirements{Title: value.Title, Summary: value.Summary, UserSetup: value.UserSetup, Acceptance: value.Acceptance, Design: agentFlowDesignReference(value.Design)}
	for _, feature := range value.Features {
		out.Features = append(out.Features, &pb.AgentRequirementFeature{Id: feature.ID, Text: feature.Text})
	}
	for _, connection := range value.Connections {
		out.Connections = append(out.Connections, &pb.AgentRequirementConnection{Need: connection.Need, Connector: connection.Connector, Operations: connection.Operations,
			Status: boundary.Enum[pb.AgentRequirementConnection_Status](connection.Status), UserSetup: connection.UserSetup})
	}
	return out
}

// agentRequirementsDraft maps a draft of the requirements card the agent is
// writing, which the RequirementsDraft Stream already bounded.
func agentRequirementsDraft(value agentloop.RequirementsDraft) *pb.AgentRequirementsDraft {
	out := &pb.AgentRequirementsDraft{CallId: string(value.CallID), Turn: value.Turn, BaseRevision: value.BaseRevision, Title: value.Title, Summary: value.Summary,
		DesignSummary: value.DesignSummary, Complete: value.Complete}
	for _, feature := range value.Features {
		out.Features = append(out.Features, &pb.AgentRequirementFeature{Id: feature.ID, Text: feature.Text})
	}
	for _, connection := range value.Connections {
		out.Connections = append(out.Connections, &pb.AgentRequirementsDraftConnection{Need: connection.Need, Connector: connection.Connector})
	}
	for _, flow := range value.Flows {
		out.Flows = append(out.Flows, &pb.AgentRequirementsDraftFlow{FlowType: flow.FlowType, StepTypes: flow.StepTypes, FailureHandlingStepTypes: flow.FailureHandlingStepTypes})
	}
	return out
}

// agentActivity preserves causal input metadata, including meaningful zero values.
func agentActivity(value conversation.AgentEvent) *pb.AgentEvent {
	out := &pb.AgentEvent{Kind: boundary.Enum[pb.AgentEvent_Kind](value.Kind), Message: value.Message, CallId: agentStringPointer(value.CallID), ToolName: agentStringPointer(value.ToolName), MessageSequence: agentInt64Pointer(value.MessageSequence), Attempt: value.Attempt, Target: value.Target}
	if p := value.InputConsumption; p != nil {
		out.InputConsumption = &pb.AgentInputConsumption{}
		for _, id := range p.QueuedMessageIDs {
			out.InputConsumption.QueuedMessageIds = append(out.InputConsumption.QueuedMessageIds, string(id))
		}
		for _, id := range p.SteeredMessageIDs {
			out.InputConsumption.SteeredMessageIds = append(out.InputConsumption.SteeredMessageIds, string(id))
		}
	}
	return out
}

func agentPendingMessages(values []conversation.PendingUserMessage) []*pb.AgentPendingUserMessage {
	out := make([]*pb.AgentPendingUserMessage, 0, len(values))
	for _, value := range values {
		var summaries []conversation.AttachmentSummary
		for _, attachment := range value.Value.Attachments {
			summaries = append(summaries, attachment.Summary())
		}
		out = append(out, &pb.AgentPendingUserMessage{MessageId: string(value.MessageID), SteeredBy: agentActor(value.SteeredBy),
			Value: &pb.AgentUserMessage{Content: value.Value.Content, Actor: agentActor(value.Value.Actor), Attachments: agentAttachments(summaries)}})
	}
	return out
}

// agentAttachments maps what a message shows of its attachments: never their
// objects or content.
func agentAttachments(values []conversation.AttachmentSummary) []*pb.AgentAttachment {
	var out []*pb.AgentAttachment
	for _, value := range values {
		out = append(out, &pb.AgentAttachment{AttachmentId: value.ID, FileName: value.Name, MediaType: value.MediaType, Kind: boundary.Enum[pb.AttachmentKind](value.Kind),
			ByteSize: value.ByteSize, Width: int32(value.Width), Height: int32(value.Height), JsonFormat: string(value.JSONFormat), //nolint:gosec // an image is at most 8000 px per side
			Redactions: int32(value.Redactions)}) //nolint:gosec // a JSON file of at most 4 MiB holds fewer values
	}
	return out
}

// agentActor maps only the frozen, server-authenticated author.
func agentActor(value *conversation.Actor) *pb.Actor {
	if value == nil {
		return nil
	}
	return &pb.Actor{AccountId: value.AccountID, Email: value.Email}
}

func agentStringPointer[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	result := string(*value)
	return &result
}

func agentInt64Pointer[T ~int64](value *T) *int64 {
	if value == nil {
		return nil
	}
	result := int64(*value)
	return &result
}

func agentDatePointer(value *time.Time) *string {
	if value == nil || value.IsZero() {
		return nil
	}
	result := value.UTC().Format(time.RFC3339Nano)
	return &result
}

// agentFlowDesignReference maps a card's design reference; nil stays nil.
func agentFlowDesignReference(value *flowdesign.Reference) *pb.AgentFlowDesignReference {
	if value == nil {
		return nil
	}
	return &pb.AgentFlowDesignReference{Digest: value.Digest, Summary: value.Summary, Outline: value.Outline}
}

// agentFlowDesign maps a design read at digest.
func agentFlowDesign(design flowdesign.Design, digest string) *pb.AgentFlowDesign {
	out := &pb.AgentFlowDesign{Digest: digest, Summary: design.Summary, RemovedFlows: design.RemovedFlows}
	for _, flow := range design.Flows {
		item := &pb.AgentFlowDesignFlow{FlowType: flow.FlowType, Purpose: flow.Purpose, StartStep: flow.StartStep, Starts: flow.Starts}
		for _, step := range flow.Steps {
			designed := &pb.AgentFlowDesignStep{StepType: step.StepType, Group: step.Group, Explanation: step.Explanation, Features: step.Features,
				WaitsFor: &pb.AgentFlowDesignWait{Kind: boundary.Enum[pb.AgentFlowDesignWait_Kind](step.WaitsFor.Kind), Name: step.WaitsFor.Name}}
			for _, next := range step.Next {
				transition := &pb.AgentFlowDesignTransition{When: next.When, IsFailureRoute: next.IsFailureRoute}
				if end, isEnd := next.End(); isEnd {
					transition.End = boundary.Enum[pb.AgentFlowDesignTransition_End](end)
				} else {
					transition.ToStep = next.To
				}
				designed.Next = append(designed.Next, transition)
			}
			item.Steps = append(item.Steps, designed)
		}
		for _, channel := range flow.Channels {
			item.Channels = append(item.Channels, &pb.AgentFlowDesignChannel{Name: channel.Name, Payload: channel.Payload})
		}
		for _, action := range flow.Actions {
			item.Actions = append(item.Actions, &pb.AgentFlowDesignAction{Label: action.Label, Rpc: action.RPC, Publishes: action.Publishes, Permission: action.Permission})
		}
		for _, attribute := range flow.Attributes {
			item.Attributes = append(item.Attributes, &pb.AgentFlowDesignAttribute{Name: attribute.Name, Value: attribute.Value, IsMap: attribute.IsMap, Key: attribute.Key})
		}
		out.Flows = append(out.Flows, item)
	}
	return out
}

func sessionExecutor(value remoteexec.Executor) *pb.SessionExecutor {
	out := &pb.SessionExecutor{Mode: boundary.Enum[pb.SessionExecutorMode](value.Mode), Epoch: value.Epoch, Generation: value.Generation}
	if remote := value.Remote; remote != nil {
		out.Remote = &pb.RemoteSessionExecutor{ConnectionId: remote.ConnectionID, Owner: agentActor(&remote.Owner), Reported: remote.Reported,
			ActivatedAt: timestamppb.New(remote.ActivatedAt), Binding: &pb.RemoteAgentBinding{CodexVersion: remote.Binding.CodexVersion, ProviderThreadId: remote.Binding.ProviderThreadID, HostLabel: remote.Binding.HostLabel}}
	}
	return out
}
