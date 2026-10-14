// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { AnswerQuestionsRequest as _superverse_v2_AnswerQuestionsRequest, AnswerQuestionsRequest__Output as _superverse_v2_AnswerQuestionsRequest__Output } from '../../superverse/v2/AnswerQuestionsRequest';
import type { AnswerQuestionsResponse as _superverse_v2_AnswerQuestionsResponse, AnswerQuestionsResponse__Output as _superverse_v2_AnswerQuestionsResponse__Output } from '../../superverse/v2/AnswerQuestionsResponse';
import type { DeleteQueuedMessageRequest as _superverse_v2_DeleteQueuedMessageRequest, DeleteQueuedMessageRequest__Output as _superverse_v2_DeleteQueuedMessageRequest__Output } from '../../superverse/v2/DeleteQueuedMessageRequest';
import type { DeleteQueuedMessageResponse as _superverse_v2_DeleteQueuedMessageResponse, DeleteQueuedMessageResponse__Output as _superverse_v2_DeleteQueuedMessageResponse__Output } from '../../superverse/v2/DeleteQueuedMessageResponse';
import type { GetFlowDesignRequest as _superverse_v2_GetFlowDesignRequest, GetFlowDesignRequest__Output as _superverse_v2_GetFlowDesignRequest__Output } from '../../superverse/v2/GetFlowDesignRequest';
import type { GetFlowDesignResponse as _superverse_v2_GetFlowDesignResponse, GetFlowDesignResponse__Output as _superverse_v2_GetFlowDesignResponse__Output } from '../../superverse/v2/GetFlowDesignResponse';
import type { InterruptAgentRequest as _superverse_v2_InterruptAgentRequest, InterruptAgentRequest__Output as _superverse_v2_InterruptAgentRequest__Output } from '../../superverse/v2/InterruptAgentRequest';
import type { InterruptAgentResponse as _superverse_v2_InterruptAgentResponse, InterruptAgentResponse__Output as _superverse_v2_InterruptAgentResponse__Output } from '../../superverse/v2/InterruptAgentResponse';
import type { ListMockScenariosRequest as _superverse_v2_ListMockScenariosRequest, ListMockScenariosRequest__Output as _superverse_v2_ListMockScenariosRequest__Output } from '../../superverse/v2/ListMockScenariosRequest';
import type { ListMockScenariosResponse as _superverse_v2_ListMockScenariosResponse, ListMockScenariosResponse__Output as _superverse_v2_ListMockScenariosResponse__Output } from '../../superverse/v2/ListMockScenariosResponse';
import type { ResolveToolApprovalRequest as _superverse_v2_ResolveToolApprovalRequest, ResolveToolApprovalRequest__Output as _superverse_v2_ResolveToolApprovalRequest__Output } from '../../superverse/v2/ResolveToolApprovalRequest';
import type { ResolveToolApprovalResponse as _superverse_v2_ResolveToolApprovalResponse, ResolveToolApprovalResponse__Output as _superverse_v2_ResolveToolApprovalResponse__Output } from '../../superverse/v2/ResolveToolApprovalResponse';
import type { SendMessageRequest as _superverse_v2_SendMessageRequest, SendMessageRequest__Output as _superverse_v2_SendMessageRequest__Output } from '../../superverse/v2/SendMessageRequest';
import type { SendMessageResponse as _superverse_v2_SendMessageResponse, SendMessageResponse__Output as _superverse_v2_SendMessageResponse__Output } from '../../superverse/v2/SendMessageResponse';
import type { SteerQueuedMessageRequest as _superverse_v2_SteerQueuedMessageRequest, SteerQueuedMessageRequest__Output as _superverse_v2_SteerQueuedMessageRequest__Output } from '../../superverse/v2/SteerQueuedMessageRequest';
import type { SteerQueuedMessageResponse as _superverse_v2_SteerQueuedMessageResponse, SteerQueuedMessageResponse__Output as _superverse_v2_SteerQueuedMessageResponse__Output } from '../../superverse/v2/SteerQueuedMessageResponse';
import type { UpdateSessionModelRequest as _superverse_v2_UpdateSessionModelRequest, UpdateSessionModelRequest__Output as _superverse_v2_UpdateSessionModelRequest__Output } from '../../superverse/v2/UpdateSessionModelRequest';
import type { UpdateSessionModelResponse as _superverse_v2_UpdateSessionModelResponse, UpdateSessionModelResponse__Output as _superverse_v2_UpdateSessionModelResponse__Output } from '../../superverse/v2/UpdateSessionModelResponse';

export interface AgentServiceClient extends grpc.Client {
  AnswerQuestions(argument: _superverse_v2_AnswerQuestionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AnswerQuestionsResponse__Output>): grpc.ClientUnaryCall;
  AnswerQuestions(argument: _superverse_v2_AnswerQuestionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_AnswerQuestionsResponse__Output>): grpc.ClientUnaryCall;
  AnswerQuestions(argument: _superverse_v2_AnswerQuestionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AnswerQuestionsResponse__Output>): grpc.ClientUnaryCall;
  AnswerQuestions(argument: _superverse_v2_AnswerQuestionsRequest, callback: grpc.requestCallback<_superverse_v2_AnswerQuestionsResponse__Output>): grpc.ClientUnaryCall;
  answerQuestions(argument: _superverse_v2_AnswerQuestionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AnswerQuestionsResponse__Output>): grpc.ClientUnaryCall;
  answerQuestions(argument: _superverse_v2_AnswerQuestionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_AnswerQuestionsResponse__Output>): grpc.ClientUnaryCall;
  answerQuestions(argument: _superverse_v2_AnswerQuestionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AnswerQuestionsResponse__Output>): grpc.ClientUnaryCall;
  answerQuestions(argument: _superverse_v2_AnswerQuestionsRequest, callback: grpc.requestCallback<_superverse_v2_AnswerQuestionsResponse__Output>): grpc.ClientUnaryCall;

  DeleteQueuedMessage(argument: _superverse_v2_DeleteQueuedMessageRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  DeleteQueuedMessage(argument: _superverse_v2_DeleteQueuedMessageRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  DeleteQueuedMessage(argument: _superverse_v2_DeleteQueuedMessageRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  DeleteQueuedMessage(argument: _superverse_v2_DeleteQueuedMessageRequest, callback: grpc.requestCallback<_superverse_v2_DeleteQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  deleteQueuedMessage(argument: _superverse_v2_DeleteQueuedMessageRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  deleteQueuedMessage(argument: _superverse_v2_DeleteQueuedMessageRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  deleteQueuedMessage(argument: _superverse_v2_DeleteQueuedMessageRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  deleteQueuedMessage(argument: _superverse_v2_DeleteQueuedMessageRequest, callback: grpc.requestCallback<_superverse_v2_DeleteQueuedMessageResponse__Output>): grpc.ClientUnaryCall;

  GetFlowDesign(argument: _superverse_v2_GetFlowDesignRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDesignResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDesign(argument: _superverse_v2_GetFlowDesignRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowDesignResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDesign(argument: _superverse_v2_GetFlowDesignRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDesignResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDesign(argument: _superverse_v2_GetFlowDesignRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowDesignResponse__Output>): grpc.ClientUnaryCall;
  getFlowDesign(argument: _superverse_v2_GetFlowDesignRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDesignResponse__Output>): grpc.ClientUnaryCall;
  getFlowDesign(argument: _superverse_v2_GetFlowDesignRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowDesignResponse__Output>): grpc.ClientUnaryCall;
  getFlowDesign(argument: _superverse_v2_GetFlowDesignRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDesignResponse__Output>): grpc.ClientUnaryCall;
  getFlowDesign(argument: _superverse_v2_GetFlowDesignRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowDesignResponse__Output>): grpc.ClientUnaryCall;

  InterruptAgent(argument: _superverse_v2_InterruptAgentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InterruptAgentResponse__Output>): grpc.ClientUnaryCall;
  InterruptAgent(argument: _superverse_v2_InterruptAgentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_InterruptAgentResponse__Output>): grpc.ClientUnaryCall;
  InterruptAgent(argument: _superverse_v2_InterruptAgentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InterruptAgentResponse__Output>): grpc.ClientUnaryCall;
  InterruptAgent(argument: _superverse_v2_InterruptAgentRequest, callback: grpc.requestCallback<_superverse_v2_InterruptAgentResponse__Output>): grpc.ClientUnaryCall;
  interruptAgent(argument: _superverse_v2_InterruptAgentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InterruptAgentResponse__Output>): grpc.ClientUnaryCall;
  interruptAgent(argument: _superverse_v2_InterruptAgentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_InterruptAgentResponse__Output>): grpc.ClientUnaryCall;
  interruptAgent(argument: _superverse_v2_InterruptAgentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InterruptAgentResponse__Output>): grpc.ClientUnaryCall;
  interruptAgent(argument: _superverse_v2_InterruptAgentRequest, callback: grpc.requestCallback<_superverse_v2_InterruptAgentResponse__Output>): grpc.ClientUnaryCall;

  ListMockScenarios(argument: _superverse_v2_ListMockScenariosRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListMockScenariosResponse__Output>): grpc.ClientUnaryCall;
  ListMockScenarios(argument: _superverse_v2_ListMockScenariosRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListMockScenariosResponse__Output>): grpc.ClientUnaryCall;
  ListMockScenarios(argument: _superverse_v2_ListMockScenariosRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListMockScenariosResponse__Output>): grpc.ClientUnaryCall;
  ListMockScenarios(argument: _superverse_v2_ListMockScenariosRequest, callback: grpc.requestCallback<_superverse_v2_ListMockScenariosResponse__Output>): grpc.ClientUnaryCall;
  listMockScenarios(argument: _superverse_v2_ListMockScenariosRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListMockScenariosResponse__Output>): grpc.ClientUnaryCall;
  listMockScenarios(argument: _superverse_v2_ListMockScenariosRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListMockScenariosResponse__Output>): grpc.ClientUnaryCall;
  listMockScenarios(argument: _superverse_v2_ListMockScenariosRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListMockScenariosResponse__Output>): grpc.ClientUnaryCall;
  listMockScenarios(argument: _superverse_v2_ListMockScenariosRequest, callback: grpc.requestCallback<_superverse_v2_ListMockScenariosResponse__Output>): grpc.ClientUnaryCall;

  ResolveToolApproval(argument: _superverse_v2_ResolveToolApprovalRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ResolveToolApprovalResponse__Output>): grpc.ClientUnaryCall;
  ResolveToolApproval(argument: _superverse_v2_ResolveToolApprovalRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ResolveToolApprovalResponse__Output>): grpc.ClientUnaryCall;
  ResolveToolApproval(argument: _superverse_v2_ResolveToolApprovalRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ResolveToolApprovalResponse__Output>): grpc.ClientUnaryCall;
  ResolveToolApproval(argument: _superverse_v2_ResolveToolApprovalRequest, callback: grpc.requestCallback<_superverse_v2_ResolveToolApprovalResponse__Output>): grpc.ClientUnaryCall;
  resolveToolApproval(argument: _superverse_v2_ResolveToolApprovalRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ResolveToolApprovalResponse__Output>): grpc.ClientUnaryCall;
  resolveToolApproval(argument: _superverse_v2_ResolveToolApprovalRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ResolveToolApprovalResponse__Output>): grpc.ClientUnaryCall;
  resolveToolApproval(argument: _superverse_v2_ResolveToolApprovalRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ResolveToolApprovalResponse__Output>): grpc.ClientUnaryCall;
  resolveToolApproval(argument: _superverse_v2_ResolveToolApprovalRequest, callback: grpc.requestCallback<_superverse_v2_ResolveToolApprovalResponse__Output>): grpc.ClientUnaryCall;

  SendMessage(argument: _superverse_v2_SendMessageRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SendMessageResponse__Output>): grpc.ClientUnaryCall;
  SendMessage(argument: _superverse_v2_SendMessageRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SendMessageResponse__Output>): grpc.ClientUnaryCall;
  SendMessage(argument: _superverse_v2_SendMessageRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SendMessageResponse__Output>): grpc.ClientUnaryCall;
  SendMessage(argument: _superverse_v2_SendMessageRequest, callback: grpc.requestCallback<_superverse_v2_SendMessageResponse__Output>): grpc.ClientUnaryCall;
  sendMessage(argument: _superverse_v2_SendMessageRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SendMessageResponse__Output>): grpc.ClientUnaryCall;
  sendMessage(argument: _superverse_v2_SendMessageRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SendMessageResponse__Output>): grpc.ClientUnaryCall;
  sendMessage(argument: _superverse_v2_SendMessageRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SendMessageResponse__Output>): grpc.ClientUnaryCall;
  sendMessage(argument: _superverse_v2_SendMessageRequest, callback: grpc.requestCallback<_superverse_v2_SendMessageResponse__Output>): grpc.ClientUnaryCall;

  SteerQueuedMessage(argument: _superverse_v2_SteerQueuedMessageRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SteerQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  SteerQueuedMessage(argument: _superverse_v2_SteerQueuedMessageRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SteerQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  SteerQueuedMessage(argument: _superverse_v2_SteerQueuedMessageRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SteerQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  SteerQueuedMessage(argument: _superverse_v2_SteerQueuedMessageRequest, callback: grpc.requestCallback<_superverse_v2_SteerQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  steerQueuedMessage(argument: _superverse_v2_SteerQueuedMessageRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SteerQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  steerQueuedMessage(argument: _superverse_v2_SteerQueuedMessageRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SteerQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  steerQueuedMessage(argument: _superverse_v2_SteerQueuedMessageRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SteerQueuedMessageResponse__Output>): grpc.ClientUnaryCall;
  steerQueuedMessage(argument: _superverse_v2_SteerQueuedMessageRequest, callback: grpc.requestCallback<_superverse_v2_SteerQueuedMessageResponse__Output>): grpc.ClientUnaryCall;

  UpdateSessionModel(argument: _superverse_v2_UpdateSessionModelRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdateSessionModelResponse__Output>): grpc.ClientUnaryCall;
  UpdateSessionModel(argument: _superverse_v2_UpdateSessionModelRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_UpdateSessionModelResponse__Output>): grpc.ClientUnaryCall;
  UpdateSessionModel(argument: _superverse_v2_UpdateSessionModelRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdateSessionModelResponse__Output>): grpc.ClientUnaryCall;
  UpdateSessionModel(argument: _superverse_v2_UpdateSessionModelRequest, callback: grpc.requestCallback<_superverse_v2_UpdateSessionModelResponse__Output>): grpc.ClientUnaryCall;
  updateSessionModel(argument: _superverse_v2_UpdateSessionModelRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdateSessionModelResponse__Output>): grpc.ClientUnaryCall;
  updateSessionModel(argument: _superverse_v2_UpdateSessionModelRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_UpdateSessionModelResponse__Output>): grpc.ClientUnaryCall;
  updateSessionModel(argument: _superverse_v2_UpdateSessionModelRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdateSessionModelResponse__Output>): grpc.ClientUnaryCall;
  updateSessionModel(argument: _superverse_v2_UpdateSessionModelRequest, callback: grpc.requestCallback<_superverse_v2_UpdateSessionModelResponse__Output>): grpc.ClientUnaryCall;

}

export interface AgentServiceHandlers extends grpc.UntypedServiceImplementation {
  AnswerQuestions: grpc.handleUnaryCall<_superverse_v2_AnswerQuestionsRequest__Output, _superverse_v2_AnswerQuestionsResponse>;

  DeleteQueuedMessage: grpc.handleUnaryCall<_superverse_v2_DeleteQueuedMessageRequest__Output, _superverse_v2_DeleteQueuedMessageResponse>;

  GetFlowDesign: grpc.handleUnaryCall<_superverse_v2_GetFlowDesignRequest__Output, _superverse_v2_GetFlowDesignResponse>;

  InterruptAgent: grpc.handleUnaryCall<_superverse_v2_InterruptAgentRequest__Output, _superverse_v2_InterruptAgentResponse>;

  ListMockScenarios: grpc.handleUnaryCall<_superverse_v2_ListMockScenariosRequest__Output, _superverse_v2_ListMockScenariosResponse>;

  ResolveToolApproval: grpc.handleUnaryCall<_superverse_v2_ResolveToolApprovalRequest__Output, _superverse_v2_ResolveToolApprovalResponse>;

  SendMessage: grpc.handleUnaryCall<_superverse_v2_SendMessageRequest__Output, _superverse_v2_SendMessageResponse>;

  SteerQueuedMessage: grpc.handleUnaryCall<_superverse_v2_SteerQueuedMessageRequest__Output, _superverse_v2_SteerQueuedMessageResponse>;

  UpdateSessionModel: grpc.handleUnaryCall<_superverse_v2_UpdateSessionModelRequest__Output, _superverse_v2_UpdateSessionModelResponse>;

}

export interface AgentServiceDefinition extends grpc.ServiceDefinition {
  AnswerQuestions: MethodDefinition<_superverse_v2_AnswerQuestionsRequest, _superverse_v2_AnswerQuestionsResponse, _superverse_v2_AnswerQuestionsRequest__Output, _superverse_v2_AnswerQuestionsResponse__Output>
  DeleteQueuedMessage: MethodDefinition<_superverse_v2_DeleteQueuedMessageRequest, _superverse_v2_DeleteQueuedMessageResponse, _superverse_v2_DeleteQueuedMessageRequest__Output, _superverse_v2_DeleteQueuedMessageResponse__Output>
  GetFlowDesign: MethodDefinition<_superverse_v2_GetFlowDesignRequest, _superverse_v2_GetFlowDesignResponse, _superverse_v2_GetFlowDesignRequest__Output, _superverse_v2_GetFlowDesignResponse__Output>
  InterruptAgent: MethodDefinition<_superverse_v2_InterruptAgentRequest, _superverse_v2_InterruptAgentResponse, _superverse_v2_InterruptAgentRequest__Output, _superverse_v2_InterruptAgentResponse__Output>
  ListMockScenarios: MethodDefinition<_superverse_v2_ListMockScenariosRequest, _superverse_v2_ListMockScenariosResponse, _superverse_v2_ListMockScenariosRequest__Output, _superverse_v2_ListMockScenariosResponse__Output>
  ResolveToolApproval: MethodDefinition<_superverse_v2_ResolveToolApprovalRequest, _superverse_v2_ResolveToolApprovalResponse, _superverse_v2_ResolveToolApprovalRequest__Output, _superverse_v2_ResolveToolApprovalResponse__Output>
  SendMessage: MethodDefinition<_superverse_v2_SendMessageRequest, _superverse_v2_SendMessageResponse, _superverse_v2_SendMessageRequest__Output, _superverse_v2_SendMessageResponse__Output>
  SteerQueuedMessage: MethodDefinition<_superverse_v2_SteerQueuedMessageRequest, _superverse_v2_SteerQueuedMessageResponse, _superverse_v2_SteerQueuedMessageRequest__Output, _superverse_v2_SteerQueuedMessageResponse__Output>
  UpdateSessionModel: MethodDefinition<_superverse_v2_UpdateSessionModelRequest, _superverse_v2_UpdateSessionModelResponse, _superverse_v2_UpdateSessionModelRequest__Output, _superverse_v2_UpdateSessionModelResponse__Output>
}
