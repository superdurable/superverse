// Original file: api/superverse/v2/superverse.proto

import type { AgentModel as _superverse_v2_AgentModel, AgentModel__Output as _superverse_v2_AgentModel__Output } from '../../superverse/v2/AgentModel';
import type { AgentPendingApproval as _superverse_v2_AgentPendingApproval, AgentPendingApproval__Output as _superverse_v2_AgentPendingApproval__Output } from '../../superverse/v2/AgentPendingApproval';
import type { AgentPendingTimer as _superverse_v2_AgentPendingTimer, AgentPendingTimer__Output as _superverse_v2_AgentPendingTimer__Output } from '../../superverse/v2/AgentPendingTimer';
import type { AgentPendingUserInput as _superverse_v2_AgentPendingUserInput, AgentPendingUserInput__Output as _superverse_v2_AgentPendingUserInput__Output } from '../../superverse/v2/AgentPendingUserInput';
import type { AgentTodoList as _superverse_v2_AgentTodoList, AgentTodoList__Output as _superverse_v2_AgentTodoList__Output } from '../../superverse/v2/AgentTodoList';
import type { AgentConfirmedRequirements as _superverse_v2_AgentConfirmedRequirements, AgentConfirmedRequirements__Output as _superverse_v2_AgentConfirmedRequirements__Output } from '../../superverse/v2/AgentConfirmedRequirements';
import type { AgentBackgroundCommand as _superverse_v2_AgentBackgroundCommand, AgentBackgroundCommand__Output as _superverse_v2_AgentBackgroundCommand__Output } from '../../superverse/v2/AgentBackgroundCommand';
import type { AgentMockProgress as _superverse_v2_AgentMockProgress, AgentMockProgress__Output as _superverse_v2_AgentMockProgress__Output } from '../../superverse/v2/AgentMockProgress';
import type { AgentSubagent as _superverse_v2_AgentSubagent, AgentSubagent__Output as _superverse_v2_AgentSubagent__Output } from '../../superverse/v2/AgentSubagent';
import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { SessionExecutor as _superverse_v2_SessionExecutor, SessionExecutor__Output as _superverse_v2_SessionExecutor__Output } from '../../superverse/v2/SessionExecutor';
import type { Long } from '@grpc/proto-loader';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentDescription_Status = {
  STATUS_UNSPECIFIED: 'STATUS_UNSPECIFIED',
  STATUS_INITIALIZING: 'STATUS_INITIALIZING',
  STATUS_WAITING_FOR_MESSAGE: 'STATUS_WAITING_FOR_MESSAGE',
  STATUS_COMPACTING_CONTEXT: 'STATUS_COMPACTING_CONTEXT',
  STATUS_CALLING_MODEL: 'STATUS_CALLING_MODEL',
  STATUS_ROUTING_TOOL: 'STATUS_ROUTING_TOOL',
  STATUS_WAITING_FOR_TOOL_APPROVAL: 'STATUS_WAITING_FOR_TOOL_APPROVAL',
  STATUS_EXECUTING_TOOL: 'STATUS_EXECUTING_TOOL',
  STATUS_WAITING_FOR_TIMER: 'STATUS_WAITING_FOR_TIMER',
} as const;

export type _superverse_v2_AgentDescription_Status =
  | 'STATUS_UNSPECIFIED'
  | 0
  | 'STATUS_INITIALIZING'
  | 1
  | 'STATUS_WAITING_FOR_MESSAGE'
  | 2
  | 'STATUS_COMPACTING_CONTEXT'
  | 3
  | 'STATUS_CALLING_MODEL'
  | 4
  | 'STATUS_ROUTING_TOOL'
  | 5
  | 'STATUS_WAITING_FOR_TOOL_APPROVAL'
  | 6
  | 'STATUS_EXECUTING_TOOL'
  | 8
  | 'STATUS_WAITING_FOR_TIMER'
  | 9

export type _superverse_v2_AgentDescription_Status__Output = typeof _superverse_v2_AgentDescription_Status[keyof typeof _superverse_v2_AgentDescription_Status]

export interface AgentDescription {
  'status'?: (_superverse_v2_AgentDescription_Status);
  'waitingInputRound'?: (number | string | Long);
  'model'?: (_superverse_v2_AgentModel);
  'firstRetainedSequence'?: (number | string | Long);
  'lastSequence'?: (number | string | Long);
  'pendingApproval'?: (_superverse_v2_AgentPendingApproval | null);
  'pendingTimer'?: (_superverse_v2_AgentPendingTimer | null);
  'pendingUserInput'?: (_superverse_v2_AgentPendingUserInput | null);
  'pendingQueuedMessageCount'?: (number | string | Long);
  'pendingSteeredMessageCount'?: (number | string | Long);
  'availableTools'?: (string)[];
  'modelRevision'?: (number | string | Long);
  'effort'?: (string);
  'todoList'?: (_superverse_v2_AgentTodoList | null);
  'contextModel'?: (_superverse_v2_AgentModel);
  'confirmedRequirements'?: (_superverse_v2_AgentConfirmedRequirements | null);
  'turn'?: (number | string | Long);
  'backgroundCommands'?: (_superverse_v2_AgentBackgroundCommand)[];
  'mockProgress'?: (_superverse_v2_AgentMockProgress | null);
  'subagents'?: (_superverse_v2_AgentSubagent)[];
  'isPreviewAutoDeployEnabled'?: (boolean);
  'modelChangedBy'?: (_superverse_v2_Actor | null);
  'previewAutoDeployChangedBy'?: (_superverse_v2_Actor | null);
  'executor'?: (_superverse_v2_SessionExecutor | null);
}

export interface AgentDescription__Output {
  'status': (_superverse_v2_AgentDescription_Status__Output);
  'waitingInputRound': (string);
  'model': (_superverse_v2_AgentModel__Output);
  'firstRetainedSequence': (string);
  'lastSequence': (string);
  'pendingApproval': (_superverse_v2_AgentPendingApproval__Output | null);
  'pendingTimer': (_superverse_v2_AgentPendingTimer__Output | null);
  'pendingUserInput': (_superverse_v2_AgentPendingUserInput__Output | null);
  'pendingQueuedMessageCount': (string);
  'pendingSteeredMessageCount': (string);
  'availableTools': (string)[];
  'modelRevision': (string);
  'effort': (string);
  'todoList': (_superverse_v2_AgentTodoList__Output | null);
  'contextModel': (_superverse_v2_AgentModel__Output);
  'confirmedRequirements': (_superverse_v2_AgentConfirmedRequirements__Output | null);
  'turn': (string);
  'backgroundCommands': (_superverse_v2_AgentBackgroundCommand__Output)[];
  'mockProgress': (_superverse_v2_AgentMockProgress__Output | null);
  'subagents': (_superverse_v2_AgentSubagent__Output)[];
  'isPreviewAutoDeployEnabled': (boolean);
  'modelChangedBy': (_superverse_v2_Actor__Output | null);
  'previewAutoDeployChangedBy': (_superverse_v2_Actor__Output | null);
  'executor': (_superverse_v2_SessionExecutor__Output | null);
}
