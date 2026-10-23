import type * as grpc from '@grpc/grpc-js';
import type { EnumTypeDefinition, MessageTypeDefinition } from '@grpc/proto-loader';

import type { ListValue as _google_protobuf_ListValue, ListValue__Output as _google_protobuf_ListValue__Output } from './google/protobuf/ListValue';
import type { Struct as _google_protobuf_Struct, Struct__Output as _google_protobuf_Struct__Output } from './google/protobuf/Struct';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from './google/protobuf/Timestamp';
import type { Value as _google_protobuf_Value, Value__Output as _google_protobuf_Value__Output } from './google/protobuf/Value';
import type { AccountLogin as _superverse_v2_AccountLogin, AccountLogin__Output as _superverse_v2_AccountLogin__Output } from './superverse/v2/AccountLogin';
import type { AccountServiceClient as _superverse_v2_AccountServiceClient, AccountServiceDefinition as _superverse_v2_AccountServiceDefinition } from './superverse/v2/AccountService';
import type { AccountSnapshot as _superverse_v2_AccountSnapshot, AccountSnapshot__Output as _superverse_v2_AccountSnapshot__Output } from './superverse/v2/AccountSnapshot';
import type { ActiveDeploymentReference as _superverse_v2_ActiveDeploymentReference, ActiveDeploymentReference__Output as _superverse_v2_ActiveDeploymentReference__Output } from './superverse/v2/ActiveDeploymentReference';
import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from './superverse/v2/Actor';
import type { AddProjectMemberRequest as _superverse_v2_AddProjectMemberRequest, AddProjectMemberRequest__Output as _superverse_v2_AddProjectMemberRequest__Output } from './superverse/v2/AddProjectMemberRequest';
import type { AddProjectMemberResponse as _superverse_v2_AddProjectMemberResponse, AddProjectMemberResponse__Output as _superverse_v2_AddProjectMemberResponse__Output } from './superverse/v2/AddProjectMemberResponse';
import type { AddTeamSpaceMemberRequest as _superverse_v2_AddTeamSpaceMemberRequest, AddTeamSpaceMemberRequest__Output as _superverse_v2_AddTeamSpaceMemberRequest__Output } from './superverse/v2/AddTeamSpaceMemberRequest';
import type { AddTeamSpaceMemberResponse as _superverse_v2_AddTeamSpaceMemberResponse, AddTeamSpaceMemberResponse__Output as _superverse_v2_AddTeamSpaceMemberResponse__Output } from './superverse/v2/AddTeamSpaceMemberResponse';
import type { AgentAnswerQuestionsInput as _superverse_v2_AgentAnswerQuestionsInput, AgentAnswerQuestionsInput__Output as _superverse_v2_AgentAnswerQuestionsInput__Output } from './superverse/v2/AgentAnswerQuestionsInput';
import type { AgentAttachment as _superverse_v2_AgentAttachment, AgentAttachment__Output as _superverse_v2_AgentAttachment__Output } from './superverse/v2/AgentAttachment';
import type { AgentBackgroundCommand as _superverse_v2_AgentBackgroundCommand, AgentBackgroundCommand__Output as _superverse_v2_AgentBackgroundCommand__Output } from './superverse/v2/AgentBackgroundCommand';
import type { AgentConfirmedRequirements as _superverse_v2_AgentConfirmedRequirements, AgentConfirmedRequirements__Output as _superverse_v2_AgentConfirmedRequirements__Output } from './superverse/v2/AgentConfirmedRequirements';
import type { AgentDescription as _superverse_v2_AgentDescription, AgentDescription__Output as _superverse_v2_AgentDescription__Output } from './superverse/v2/AgentDescription';
import type { AgentEvent as _superverse_v2_AgentEvent, AgentEvent__Output as _superverse_v2_AgentEvent__Output } from './superverse/v2/AgentEvent';
import type { AgentFlowDesign as _superverse_v2_AgentFlowDesign, AgentFlowDesign__Output as _superverse_v2_AgentFlowDesign__Output } from './superverse/v2/AgentFlowDesign';
import type { AgentFlowDesignAction as _superverse_v2_AgentFlowDesignAction, AgentFlowDesignAction__Output as _superverse_v2_AgentFlowDesignAction__Output } from './superverse/v2/AgentFlowDesignAction';
import type { AgentFlowDesignAttribute as _superverse_v2_AgentFlowDesignAttribute, AgentFlowDesignAttribute__Output as _superverse_v2_AgentFlowDesignAttribute__Output } from './superverse/v2/AgentFlowDesignAttribute';
import type { AgentFlowDesignChannel as _superverse_v2_AgentFlowDesignChannel, AgentFlowDesignChannel__Output as _superverse_v2_AgentFlowDesignChannel__Output } from './superverse/v2/AgentFlowDesignChannel';
import type { AgentFlowDesignFlow as _superverse_v2_AgentFlowDesignFlow, AgentFlowDesignFlow__Output as _superverse_v2_AgentFlowDesignFlow__Output } from './superverse/v2/AgentFlowDesignFlow';
import type { AgentFlowDesignReference as _superverse_v2_AgentFlowDesignReference, AgentFlowDesignReference__Output as _superverse_v2_AgentFlowDesignReference__Output } from './superverse/v2/AgentFlowDesignReference';
import type { AgentFlowDesignStep as _superverse_v2_AgentFlowDesignStep, AgentFlowDesignStep__Output as _superverse_v2_AgentFlowDesignStep__Output } from './superverse/v2/AgentFlowDesignStep';
import type { AgentFlowDesignTransition as _superverse_v2_AgentFlowDesignTransition, AgentFlowDesignTransition__Output as _superverse_v2_AgentFlowDesignTransition__Output } from './superverse/v2/AgentFlowDesignTransition';
import type { AgentFlowDesignWait as _superverse_v2_AgentFlowDesignWait, AgentFlowDesignWait__Output as _superverse_v2_AgentFlowDesignWait__Output } from './superverse/v2/AgentFlowDesignWait';
import type { AgentHistoryPage as _superverse_v2_AgentHistoryPage, AgentHistoryPage__Output as _superverse_v2_AgentHistoryPage__Output } from './superverse/v2/AgentHistoryPage';
import type { AgentInputConsumption as _superverse_v2_AgentInputConsumption, AgentInputConsumption__Output as _superverse_v2_AgentInputConsumption__Output } from './superverse/v2/AgentInputConsumption';
import type { AgentMessage as _superverse_v2_AgentMessage, AgentMessage__Output as _superverse_v2_AgentMessage__Output } from './superverse/v2/AgentMessage';
import type { AgentMockProgress as _superverse_v2_AgentMockProgress, AgentMockProgress__Output as _superverse_v2_AgentMockProgress__Output } from './superverse/v2/AgentMockProgress';
import type { AgentMockScenario as _superverse_v2_AgentMockScenario, AgentMockScenario__Output as _superverse_v2_AgentMockScenario__Output } from './superverse/v2/AgentMockScenario';
import type { AgentMockScenarioArgument as _superverse_v2_AgentMockScenarioArgument, AgentMockScenarioArgument__Output as _superverse_v2_AgentMockScenarioArgument__Output } from './superverse/v2/AgentMockScenarioArgument';
import type { AgentMockScenarioInput as _superverse_v2_AgentMockScenarioInput, AgentMockScenarioInput__Output as _superverse_v2_AgentMockScenarioInput__Output } from './superverse/v2/AgentMockScenarioInput';
import type { AgentModelCallMetrics as _superverse_v2_AgentModelCallMetrics, AgentModelCallMetrics__Output as _superverse_v2_AgentModelCallMetrics__Output } from './superverse/v2/AgentModelCallMetrics';
import type { AgentModelChoice as _superverse_v2_AgentModelChoice, AgentModelChoice__Output as _superverse_v2_AgentModelChoice__Output } from './superverse/v2/AgentModelChoice';
import type { AgentNotification as _superverse_v2_AgentNotification, AgentNotification__Output as _superverse_v2_AgentNotification__Output } from './superverse/v2/AgentNotification';
import type { AgentPendingApproval as _superverse_v2_AgentPendingApproval, AgentPendingApproval__Output as _superverse_v2_AgentPendingApproval__Output } from './superverse/v2/AgentPendingApproval';
import type { AgentPendingTimer as _superverse_v2_AgentPendingTimer, AgentPendingTimer__Output as _superverse_v2_AgentPendingTimer__Output } from './superverse/v2/AgentPendingTimer';
import type { AgentPendingUserInput as _superverse_v2_AgentPendingUserInput, AgentPendingUserInput__Output as _superverse_v2_AgentPendingUserInput__Output } from './superverse/v2/AgentPendingUserInput';
import type { AgentPendingUserMessage as _superverse_v2_AgentPendingUserMessage, AgentPendingUserMessage__Output as _superverse_v2_AgentPendingUserMessage__Output } from './superverse/v2/AgentPendingUserMessage';
import type { AgentRequirementConnection as _superverse_v2_AgentRequirementConnection, AgentRequirementConnection__Output as _superverse_v2_AgentRequirementConnection__Output } from './superverse/v2/AgentRequirementConnection';
import type { AgentRequirementFeature as _superverse_v2_AgentRequirementFeature, AgentRequirementFeature__Output as _superverse_v2_AgentRequirementFeature__Output } from './superverse/v2/AgentRequirementFeature';
import type { AgentRequirements as _superverse_v2_AgentRequirements, AgentRequirements__Output as _superverse_v2_AgentRequirements__Output } from './superverse/v2/AgentRequirements';
import type { AgentRequirementsDraft as _superverse_v2_AgentRequirementsDraft, AgentRequirementsDraft__Output as _superverse_v2_AgentRequirementsDraft__Output } from './superverse/v2/AgentRequirementsDraft';
import type { AgentRequirementsDraftConnection as _superverse_v2_AgentRequirementsDraftConnection, AgentRequirementsDraftConnection__Output as _superverse_v2_AgentRequirementsDraftConnection__Output } from './superverse/v2/AgentRequirementsDraftConnection';
import type { AgentRequirementsDraftFlow as _superverse_v2_AgentRequirementsDraftFlow, AgentRequirementsDraftFlow__Output as _superverse_v2_AgentRequirementsDraftFlow__Output } from './superverse/v2/AgentRequirementsDraftFlow';
import type { AgentSequencedMessage as _superverse_v2_AgentSequencedMessage, AgentSequencedMessage__Output as _superverse_v2_AgentSequencedMessage__Output } from './superverse/v2/AgentSequencedMessage';
import type { AgentServiceClient as _superverse_v2_AgentServiceClient, AgentServiceDefinition as _superverse_v2_AgentServiceDefinition } from './superverse/v2/AgentService';
import type { AgentSnapshot as _superverse_v2_AgentSnapshot, AgentSnapshot__Output as _superverse_v2_AgentSnapshot__Output } from './superverse/v2/AgentSnapshot';
import type { AgentStartingExample as _superverse_v2_AgentStartingExample, AgentStartingExample__Output as _superverse_v2_AgentStartingExample__Output } from './superverse/v2/AgentStartingExample';
import type { AgentStartingExampleBuild as _superverse_v2_AgentStartingExampleBuild, AgentStartingExampleBuild__Output as _superverse_v2_AgentStartingExampleBuild__Output } from './superverse/v2/AgentStartingExampleBuild';
import type { AgentSubagent as _superverse_v2_AgentSubagent, AgentSubagent__Output as _superverse_v2_AgentSubagent__Output } from './superverse/v2/AgentSubagent';
import type { AgentTodoItem as _superverse_v2_AgentTodoItem, AgentTodoItem__Output as _superverse_v2_AgentTodoItem__Output } from './superverse/v2/AgentTodoItem';
import type { AgentTodoList as _superverse_v2_AgentTodoList, AgentTodoList__Output as _superverse_v2_AgentTodoList__Output } from './superverse/v2/AgentTodoList';
import type { AgentToolCall as _superverse_v2_AgentToolCall, AgentToolCall__Output as _superverse_v2_AgentToolCall__Output } from './superverse/v2/AgentToolCall';
import type { AgentToolExecutionMetrics as _superverse_v2_AgentToolExecutionMetrics, AgentToolExecutionMetrics__Output as _superverse_v2_AgentToolExecutionMetrics__Output } from './superverse/v2/AgentToolExecutionMetrics';
import type { AgentUserInputAnswer as _superverse_v2_AgentUserInputAnswer, AgentUserInputAnswer__Output as _superverse_v2_AgentUserInputAnswer__Output } from './superverse/v2/AgentUserInputAnswer';
import type { AgentUserInputOption as _superverse_v2_AgentUserInputOption, AgentUserInputOption__Output as _superverse_v2_AgentUserInputOption__Output } from './superverse/v2/AgentUserInputOption';
import type { AgentUserInputQuestion as _superverse_v2_AgentUserInputQuestion, AgentUserInputQuestion__Output as _superverse_v2_AgentUserInputQuestion__Output } from './superverse/v2/AgentUserInputQuestion';
import type { AgentUserMessage as _superverse_v2_AgentUserMessage, AgentUserMessage__Output as _superverse_v2_AgentUserMessage__Output } from './superverse/v2/AgentUserMessage';
import type { AnswerQuestionsRequest as _superverse_v2_AnswerQuestionsRequest, AnswerQuestionsRequest__Output as _superverse_v2_AnswerQuestionsRequest__Output } from './superverse/v2/AnswerQuestionsRequest';
import type { AnswerQuestionsResponse as _superverse_v2_AnswerQuestionsResponse, AnswerQuestionsResponse__Output as _superverse_v2_AnswerQuestionsResponse__Output } from './superverse/v2/AnswerQuestionsResponse';
import type { AppManifestSnapshot as _superverse_v2_AppManifestSnapshot, AppManifestSnapshot__Output as _superverse_v2_AppManifestSnapshot__Output } from './superverse/v2/AppManifestSnapshot';
import type { ApplicationEnvironmentServiceClient as _superverse_v2_ApplicationEnvironmentServiceClient, ApplicationEnvironmentServiceDefinition as _superverse_v2_ApplicationEnvironmentServiceDefinition } from './superverse/v2/ApplicationEnvironmentService';
import type { AppliedPreviewConfiguration as _superverse_v2_AppliedPreviewConfiguration, AppliedPreviewConfiguration__Output as _superverse_v2_AppliedPreviewConfiguration__Output } from './superverse/v2/AppliedPreviewConfiguration';
import type { Attachment as _superverse_v2_Attachment, Attachment__Output as _superverse_v2_Attachment__Output } from './superverse/v2/Attachment';
import type { AttachmentDownload as _superverse_v2_AttachmentDownload, AttachmentDownload__Output as _superverse_v2_AttachmentDownload__Output } from './superverse/v2/AttachmentDownload';
import type { AttachmentFailure as _superverse_v2_AttachmentFailure, AttachmentFailure__Output as _superverse_v2_AttachmentFailure__Output } from './superverse/v2/AttachmentFailure';
import type { AttachmentServiceClient as _superverse_v2_AttachmentServiceClient, AttachmentServiceDefinition as _superverse_v2_AttachmentServiceDefinition } from './superverse/v2/AttachmentService';
import type { AttachmentSummary as _superverse_v2_AttachmentSummary, AttachmentSummary__Output as _superverse_v2_AttachmentSummary__Output } from './superverse/v2/AttachmentSummary';
import type { AttachmentUploadTarget as _superverse_v2_AttachmentUploadTarget, AttachmentUploadTarget__Output as _superverse_v2_AttachmentUploadTarget__Output } from './superverse/v2/AttachmentUploadTarget';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from './superverse/v2/CallerContext';
import type { CancelDeploymentRequest as _superverse_v2_CancelDeploymentRequest, CancelDeploymentRequest__Output as _superverse_v2_CancelDeploymentRequest__Output } from './superverse/v2/CancelDeploymentRequest';
import type { CancelDeploymentResponse as _superverse_v2_CancelDeploymentResponse, CancelDeploymentResponse__Output as _superverse_v2_CancelDeploymentResponse__Output } from './superverse/v2/CancelDeploymentResponse';
import type { CancelReleaseBuildRequest as _superverse_v2_CancelReleaseBuildRequest, CancelReleaseBuildRequest__Output as _superverse_v2_CancelReleaseBuildRequest__Output } from './superverse/v2/CancelReleaseBuildRequest';
import type { CancelReleaseBuildResponse as _superverse_v2_CancelReleaseBuildResponse, CancelReleaseBuildResponse__Output as _superverse_v2_CancelReleaseBuildResponse__Output } from './superverse/v2/CancelReleaseBuildResponse';
import type { Cancellation as _superverse_v2_Cancellation, Cancellation__Output as _superverse_v2_Cancellation__Output } from './superverse/v2/Cancellation';
import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from './superverse/v2/CommitVersion';
import type { CompleteAttachmentUploadRequest as _superverse_v2_CompleteAttachmentUploadRequest, CompleteAttachmentUploadRequest__Output as _superverse_v2_CompleteAttachmentUploadRequest__Output } from './superverse/v2/CompleteAttachmentUploadRequest';
import type { CompleteAttachmentUploadResponse as _superverse_v2_CompleteAttachmentUploadResponse, CompleteAttachmentUploadResponse__Output as _superverse_v2_CompleteAttachmentUploadResponse__Output } from './superverse/v2/CompleteAttachmentUploadResponse';
import type { CompleteTeamSpaceConnectionOAuthRequest as _superverse_v2_CompleteTeamSpaceConnectionOAuthRequest, CompleteTeamSpaceConnectionOAuthRequest__Output as _superverse_v2_CompleteTeamSpaceConnectionOAuthRequest__Output } from './superverse/v2/CompleteTeamSpaceConnectionOAuthRequest';
import type { CompleteTeamSpaceConnectionOAuthResponse as _superverse_v2_CompleteTeamSpaceConnectionOAuthResponse, CompleteTeamSpaceConnectionOAuthResponse__Output as _superverse_v2_CompleteTeamSpaceConnectionOAuthResponse__Output } from './superverse/v2/CompleteTeamSpaceConnectionOAuthResponse';
import type { ConfigurationItem as _superverse_v2_ConfigurationItem, ConfigurationItem__Output as _superverse_v2_ConfigurationItem__Output } from './superverse/v2/ConfigurationItem';
import type { ConfigurationItemKey as _superverse_v2_ConfigurationItemKey, ConfigurationItemKey__Output as _superverse_v2_ConfigurationItemKey__Output } from './superverse/v2/ConfigurationItemKey';
import type { ConfigurationRequirementGap as _superverse_v2_ConfigurationRequirementGap, ConfigurationRequirementGap__Output as _superverse_v2_ConfigurationRequirementGap__Output } from './superverse/v2/ConfigurationRequirementGap';
import type { ConnectorCatalogEntry as _superverse_v2_ConnectorCatalogEntry, ConnectorCatalogEntry__Output as _superverse_v2_ConnectorCatalogEntry__Output } from './superverse/v2/ConnectorCatalogEntry';
import type { ConnectorContribution as _superverse_v2_ConnectorContribution, ConnectorContribution__Output as _superverse_v2_ConnectorContribution__Output } from './superverse/v2/ConnectorContribution';
import type { ConnectorRequirement as _superverse_v2_ConnectorRequirement, ConnectorRequirement__Output as _superverse_v2_ConnectorRequirement__Output } from './superverse/v2/ConnectorRequirement';
import type { ConnectorServiceClient as _superverse_v2_ConnectorServiceClient, ConnectorServiceDefinition as _superverse_v2_ConnectorServiceDefinition } from './superverse/v2/ConnectorService';
import type { ConnectorTriggerBinding as _superverse_v2_ConnectorTriggerBinding, ConnectorTriggerBinding__Output as _superverse_v2_ConnectorTriggerBinding__Output } from './superverse/v2/ConnectorTriggerBinding';
import type { CreateAttachmentUploadRequest as _superverse_v2_CreateAttachmentUploadRequest, CreateAttachmentUploadRequest__Output as _superverse_v2_CreateAttachmentUploadRequest__Output } from './superverse/v2/CreateAttachmentUploadRequest';
import type { CreateAttachmentUploadResponse as _superverse_v2_CreateAttachmentUploadResponse, CreateAttachmentUploadResponse__Output as _superverse_v2_CreateAttachmentUploadResponse__Output } from './superverse/v2/CreateAttachmentUploadResponse';
import type { CreateProjectRequest as _superverse_v2_CreateProjectRequest, CreateProjectRequest__Output as _superverse_v2_CreateProjectRequest__Output } from './superverse/v2/CreateProjectRequest';
import type { CreateProjectResponse as _superverse_v2_CreateProjectResponse, CreateProjectResponse__Output as _superverse_v2_CreateProjectResponse__Output } from './superverse/v2/CreateProjectResponse';
import type { CreateRemoteAgentPairingRequest as _superverse_v2_CreateRemoteAgentPairingRequest, CreateRemoteAgentPairingRequest__Output as _superverse_v2_CreateRemoteAgentPairingRequest__Output } from './superverse/v2/CreateRemoteAgentPairingRequest';
import type { CreateRemoteAgentPairingResponse as _superverse_v2_CreateRemoteAgentPairingResponse, CreateRemoteAgentPairingResponse__Output as _superverse_v2_CreateRemoteAgentPairingResponse__Output } from './superverse/v2/CreateRemoteAgentPairingResponse';
import type { CreateTeamSpaceConnectionRequest as _superverse_v2_CreateTeamSpaceConnectionRequest, CreateTeamSpaceConnectionRequest__Output as _superverse_v2_CreateTeamSpaceConnectionRequest__Output } from './superverse/v2/CreateTeamSpaceConnectionRequest';
import type { CreateTeamSpaceConnectionResponse as _superverse_v2_CreateTeamSpaceConnectionResponse, CreateTeamSpaceConnectionResponse__Output as _superverse_v2_CreateTeamSpaceConnectionResponse__Output } from './superverse/v2/CreateTeamSpaceConnectionResponse';
import type { CreateTeamSpaceRequest as _superverse_v2_CreateTeamSpaceRequest, CreateTeamSpaceRequest__Output as _superverse_v2_CreateTeamSpaceRequest__Output } from './superverse/v2/CreateTeamSpaceRequest';
import type { CreateTeamSpaceResponse as _superverse_v2_CreateTeamSpaceResponse, CreateTeamSpaceResponse__Output as _superverse_v2_CreateTeamSpaceResponse__Output } from './superverse/v2/CreateTeamSpaceResponse';
import type { DecideEvolveProposalRequest as _superverse_v2_DecideEvolveProposalRequest, DecideEvolveProposalRequest__Output as _superverse_v2_DecideEvolveProposalRequest__Output } from './superverse/v2/DecideEvolveProposalRequest';
import type { DecideEvolveProposalResponse as _superverse_v2_DecideEvolveProposalResponse, DecideEvolveProposalResponse__Output as _superverse_v2_DecideEvolveProposalResponse__Output } from './superverse/v2/DecideEvolveProposalResponse';
import type { DeleteAttachmentRequest as _superverse_v2_DeleteAttachmentRequest, DeleteAttachmentRequest__Output as _superverse_v2_DeleteAttachmentRequest__Output } from './superverse/v2/DeleteAttachmentRequest';
import type { DeleteAttachmentResponse as _superverse_v2_DeleteAttachmentResponse, DeleteAttachmentResponse__Output as _superverse_v2_DeleteAttachmentResponse__Output } from './superverse/v2/DeleteAttachmentResponse';
import type { DeleteFlowChannelMessageRequest as _superverse_v2_DeleteFlowChannelMessageRequest, DeleteFlowChannelMessageRequest__Output as _superverse_v2_DeleteFlowChannelMessageRequest__Output } from './superverse/v2/DeleteFlowChannelMessageRequest';
import type { DeleteFlowChannelMessageResponse as _superverse_v2_DeleteFlowChannelMessageResponse, DeleteFlowChannelMessageResponse__Output as _superverse_v2_DeleteFlowChannelMessageResponse__Output } from './superverse/v2/DeleteFlowChannelMessageResponse';
import type { DeleteModelCredentialRequest as _superverse_v2_DeleteModelCredentialRequest, DeleteModelCredentialRequest__Output as _superverse_v2_DeleteModelCredentialRequest__Output } from './superverse/v2/DeleteModelCredentialRequest';
import type { DeleteModelCredentialResponse as _superverse_v2_DeleteModelCredentialResponse, DeleteModelCredentialResponse__Output as _superverse_v2_DeleteModelCredentialResponse__Output } from './superverse/v2/DeleteModelCredentialResponse';
import type { DeleteProjectRequest as _superverse_v2_DeleteProjectRequest, DeleteProjectRequest__Output as _superverse_v2_DeleteProjectRequest__Output } from './superverse/v2/DeleteProjectRequest';
import type { DeleteProjectResponse as _superverse_v2_DeleteProjectResponse, DeleteProjectResponse__Output as _superverse_v2_DeleteProjectResponse__Output } from './superverse/v2/DeleteProjectResponse';
import type { DeleteQueuedMessageRequest as _superverse_v2_DeleteQueuedMessageRequest, DeleteQueuedMessageRequest__Output as _superverse_v2_DeleteQueuedMessageRequest__Output } from './superverse/v2/DeleteQueuedMessageRequest';
import type { DeleteQueuedMessageResponse as _superverse_v2_DeleteQueuedMessageResponse, DeleteQueuedMessageResponse__Output as _superverse_v2_DeleteQueuedMessageResponse__Output } from './superverse/v2/DeleteQueuedMessageResponse';
import type { DeleteTeamSpaceConnectionRequest as _superverse_v2_DeleteTeamSpaceConnectionRequest, DeleteTeamSpaceConnectionRequest__Output as _superverse_v2_DeleteTeamSpaceConnectionRequest__Output } from './superverse/v2/DeleteTeamSpaceConnectionRequest';
import type { DeleteTeamSpaceConnectionResponse as _superverse_v2_DeleteTeamSpaceConnectionResponse, DeleteTeamSpaceConnectionResponse__Output as _superverse_v2_DeleteTeamSpaceConnectionResponse__Output } from './superverse/v2/DeleteTeamSpaceConnectionResponse';
import type { DeleteTeamSpaceRequest as _superverse_v2_DeleteTeamSpaceRequest, DeleteTeamSpaceRequest__Output as _superverse_v2_DeleteTeamSpaceRequest__Output } from './superverse/v2/DeleteTeamSpaceRequest';
import type { DeleteTeamSpaceResponse as _superverse_v2_DeleteTeamSpaceResponse, DeleteTeamSpaceResponse__Output as _superverse_v2_DeleteTeamSpaceResponse__Output } from './superverse/v2/DeleteTeamSpaceResponse';
import type { DeploymentServiceClient as _superverse_v2_DeploymentServiceClient, DeploymentServiceDefinition as _superverse_v2_DeploymentServiceDefinition } from './superverse/v2/DeploymentService';
import type { DeploymentSnapshot as _superverse_v2_DeploymentSnapshot, DeploymentSnapshot__Output as _superverse_v2_DeploymentSnapshot__Output } from './superverse/v2/DeploymentSnapshot';
import type { DeriveLiveConfigurationRequest as _superverse_v2_DeriveLiveConfigurationRequest, DeriveLiveConfigurationRequest__Output as _superverse_v2_DeriveLiveConfigurationRequest__Output } from './superverse/v2/DeriveLiveConfigurationRequest';
import type { DeriveLiveConfigurationResponse as _superverse_v2_DeriveLiveConfigurationResponse, DeriveLiveConfigurationResponse__Output as _superverse_v2_DeriveLiveConfigurationResponse__Output } from './superverse/v2/DeriveLiveConfigurationResponse';
import type { EnvironmentEvent as _superverse_v2_EnvironmentEvent, EnvironmentEvent__Output as _superverse_v2_EnvironmentEvent__Output } from './superverse/v2/EnvironmentEvent';
import type { Evolve as _superverse_v2_Evolve, Evolve__Output as _superverse_v2_Evolve__Output } from './superverse/v2/Evolve';
import type { EvolveExport as _superverse_v2_EvolveExport, EvolveExport__Output as _superverse_v2_EvolveExport__Output } from './superverse/v2/EvolveExport';
import type { EvolveGoal as _superverse_v2_EvolveGoal, EvolveGoal__Output as _superverse_v2_EvolveGoal__Output } from './superverse/v2/EvolveGoal';
import type { EvolveGoalValue as _superverse_v2_EvolveGoalValue, EvolveGoalValue__Output as _superverse_v2_EvolveGoalValue__Output } from './superverse/v2/EvolveGoalValue';
import type { EvolveGoals as _superverse_v2_EvolveGoals, EvolveGoals__Output as _superverse_v2_EvolveGoals__Output } from './superverse/v2/EvolveGoals';
import type { EvolveLive as _superverse_v2_EvolveLive, EvolveLive__Output as _superverse_v2_EvolveLive__Output } from './superverse/v2/EvolveLive';
import type { EvolveProposal as _superverse_v2_EvolveProposal, EvolveProposal__Output as _superverse_v2_EvolveProposal__Output } from './superverse/v2/EvolveProposal';
import type { EvolveProposalDecisionRecord as _superverse_v2_EvolveProposalDecisionRecord, EvolveProposalDecisionRecord__Output as _superverse_v2_EvolveProposalDecisionRecord__Output } from './superverse/v2/EvolveProposalDecisionRecord';
import type { EvolveReview as _superverse_v2_EvolveReview, EvolveReview__Output as _superverse_v2_EvolveReview__Output } from './superverse/v2/EvolveReview';
import type { EvolveSchedule as _superverse_v2_EvolveSchedule, EvolveSchedule__Output as _superverse_v2_EvolveSchedule__Output } from './superverse/v2/EvolveSchedule';
import type { EvolveServiceClient as _superverse_v2_EvolveServiceClient, EvolveServiceDefinition as _superverse_v2_EvolveServiceDefinition } from './superverse/v2/EvolveService';
import type { ExampleReference as _superverse_v2_ExampleReference, ExampleReference__Output as _superverse_v2_ExampleReference__Output } from './superverse/v2/ExampleReference';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from './superverse/v2/Failure';
import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from './superverse/v2/FlowApiContext';
import type { FlowApiDocument as _superverse_v2_FlowApiDocument, FlowApiDocument__Output as _superverse_v2_FlowApiDocument__Output } from './superverse/v2/FlowApiDocument';
import type { FlowApiTarget as _superverse_v2_FlowApiTarget, FlowApiTarget__Output as _superverse_v2_FlowApiTarget__Output } from './superverse/v2/FlowApiTarget';
import type { FlowBlobReference as _superverse_v2_FlowBlobReference, FlowBlobReference__Output as _superverse_v2_FlowBlobReference__Output } from './superverse/v2/FlowBlobReference';
import type { FlowDefinition as _superverse_v2_FlowDefinition, FlowDefinition__Output as _superverse_v2_FlowDefinition__Output } from './superverse/v2/FlowDefinition';
import type { FlowDiagnosticsServiceClient as _superverse_v2_FlowDiagnosticsServiceClient, FlowDiagnosticsServiceDefinition as _superverse_v2_FlowDiagnosticsServiceDefinition } from './superverse/v2/FlowDiagnosticsService';
import type { FlowRunServiceClient as _superverse_v2_FlowRunServiceClient, FlowRunServiceDefinition as _superverse_v2_FlowRunServiceDefinition } from './superverse/v2/FlowRunService';
import type { FlowSearchFilter as _superverse_v2_FlowSearchFilter, FlowSearchFilter__Output as _superverse_v2_FlowSearchFilter__Output } from './superverse/v2/FlowSearchFilter';
import type { GetApplicationEnvironmentRequest as _superverse_v2_GetApplicationEnvironmentRequest, GetApplicationEnvironmentRequest__Output as _superverse_v2_GetApplicationEnvironmentRequest__Output } from './superverse/v2/GetApplicationEnvironmentRequest';
import type { GetApplicationEnvironmentResponse as _superverse_v2_GetApplicationEnvironmentResponse, GetApplicationEnvironmentResponse__Output as _superverse_v2_GetApplicationEnvironmentResponse__Output } from './superverse/v2/GetApplicationEnvironmentResponse';
import type { GetAttachmentDownloadsRequest as _superverse_v2_GetAttachmentDownloadsRequest, GetAttachmentDownloadsRequest__Output as _superverse_v2_GetAttachmentDownloadsRequest__Output } from './superverse/v2/GetAttachmentDownloadsRequest';
import type { GetAttachmentDownloadsResponse as _superverse_v2_GetAttachmentDownloadsResponse, GetAttachmentDownloadsResponse__Output as _superverse_v2_GetAttachmentDownloadsResponse__Output } from './superverse/v2/GetAttachmentDownloadsResponse';
import type { GetCurrentAccountRequest as _superverse_v2_GetCurrentAccountRequest, GetCurrentAccountRequest__Output as _superverse_v2_GetCurrentAccountRequest__Output } from './superverse/v2/GetCurrentAccountRequest';
import type { GetCurrentAccountResponse as _superverse_v2_GetCurrentAccountResponse, GetCurrentAccountResponse__Output as _superverse_v2_GetCurrentAccountResponse__Output } from './superverse/v2/GetCurrentAccountResponse';
import type { GetDeploymentRequest as _superverse_v2_GetDeploymentRequest, GetDeploymentRequest__Output as _superverse_v2_GetDeploymentRequest__Output } from './superverse/v2/GetDeploymentRequest';
import type { GetDeploymentResponse as _superverse_v2_GetDeploymentResponse, GetDeploymentResponse__Output as _superverse_v2_GetDeploymentResponse__Output } from './superverse/v2/GetDeploymentResponse';
import type { GetEvolveProposalRequest as _superverse_v2_GetEvolveProposalRequest, GetEvolveProposalRequest__Output as _superverse_v2_GetEvolveProposalRequest__Output } from './superverse/v2/GetEvolveProposalRequest';
import type { GetEvolveProposalResponse as _superverse_v2_GetEvolveProposalResponse, GetEvolveProposalResponse__Output as _superverse_v2_GetEvolveProposalResponse__Output } from './superverse/v2/GetEvolveProposalResponse';
import type { GetEvolveRequest as _superverse_v2_GetEvolveRequest, GetEvolveRequest__Output as _superverse_v2_GetEvolveRequest__Output } from './superverse/v2/GetEvolveRequest';
import type { GetEvolveResponse as _superverse_v2_GetEvolveResponse, GetEvolveResponse__Output as _superverse_v2_GetEvolveResponse__Output } from './superverse/v2/GetEvolveResponse';
import type { GetFlowCatalogRequest as _superverse_v2_GetFlowCatalogRequest, GetFlowCatalogRequest__Output as _superverse_v2_GetFlowCatalogRequest__Output } from './superverse/v2/GetFlowCatalogRequest';
import type { GetFlowCatalogResponse as _superverse_v2_GetFlowCatalogResponse, GetFlowCatalogResponse__Output as _superverse_v2_GetFlowCatalogResponse__Output } from './superverse/v2/GetFlowCatalogResponse';
import type { GetFlowDefinitionsRequest as _superverse_v2_GetFlowDefinitionsRequest, GetFlowDefinitionsRequest__Output as _superverse_v2_GetFlowDefinitionsRequest__Output } from './superverse/v2/GetFlowDefinitionsRequest';
import type { GetFlowDefinitionsResponse as _superverse_v2_GetFlowDefinitionsResponse, GetFlowDefinitionsResponse__Output as _superverse_v2_GetFlowDefinitionsResponse__Output } from './superverse/v2/GetFlowDefinitionsResponse';
import type { GetFlowDesignRequest as _superverse_v2_GetFlowDesignRequest, GetFlowDesignRequest__Output as _superverse_v2_GetFlowDesignRequest__Output } from './superverse/v2/GetFlowDesignRequest';
import type { GetFlowDesignResponse as _superverse_v2_GetFlowDesignResponse, GetFlowDesignResponse__Output as _superverse_v2_GetFlowDesignResponse__Output } from './superverse/v2/GetFlowDesignResponse';
import type { GetFlowDisplayRequest as _superverse_v2_GetFlowDisplayRequest, GetFlowDisplayRequest__Output as _superverse_v2_GetFlowDisplayRequest__Output } from './superverse/v2/GetFlowDisplayRequest';
import type { GetFlowDisplayResponse as _superverse_v2_GetFlowDisplayResponse, GetFlowDisplayResponse__Output as _superverse_v2_GetFlowDisplayResponse__Output } from './superverse/v2/GetFlowDisplayResponse';
import type { GetFlowHistoryRequest as _superverse_v2_GetFlowHistoryRequest, GetFlowHistoryRequest__Output as _superverse_v2_GetFlowHistoryRequest__Output } from './superverse/v2/GetFlowHistoryRequest';
import type { GetFlowHistoryResponse as _superverse_v2_GetFlowHistoryResponse, GetFlowHistoryResponse__Output as _superverse_v2_GetFlowHistoryResponse__Output } from './superverse/v2/GetFlowHistoryResponse';
import type { GetFlowStateRequest as _superverse_v2_GetFlowStateRequest, GetFlowStateRequest__Output as _superverse_v2_GetFlowStateRequest__Output } from './superverse/v2/GetFlowStateRequest';
import type { GetFlowStateResponse as _superverse_v2_GetFlowStateResponse, GetFlowStateResponse__Output as _superverse_v2_GetFlowStateResponse__Output } from './superverse/v2/GetFlowStateResponse';
import type { GetFlowSummaryRequest as _superverse_v2_GetFlowSummaryRequest, GetFlowSummaryRequest__Output as _superverse_v2_GetFlowSummaryRequest__Output } from './superverse/v2/GetFlowSummaryRequest';
import type { GetFlowSummaryResponse as _superverse_v2_GetFlowSummaryResponse, GetFlowSummaryResponse__Output as _superverse_v2_GetFlowSummaryResponse__Output } from './superverse/v2/GetFlowSummaryResponse';
import type { GetModelCredentialInfoRequest as _superverse_v2_GetModelCredentialInfoRequest, GetModelCredentialInfoRequest__Output as _superverse_v2_GetModelCredentialInfoRequest__Output } from './superverse/v2/GetModelCredentialInfoRequest';
import type { GetModelCredentialInfoResponse as _superverse_v2_GetModelCredentialInfoResponse, GetModelCredentialInfoResponse__Output as _superverse_v2_GetModelCredentialInfoResponse__Output } from './superverse/v2/GetModelCredentialInfoResponse';
import type { GetPreviewDefinitionsRequest as _superverse_v2_GetPreviewDefinitionsRequest, GetPreviewDefinitionsRequest__Output as _superverse_v2_GetPreviewDefinitionsRequest__Output } from './superverse/v2/GetPreviewDefinitionsRequest';
import type { GetPreviewDefinitionsResponse as _superverse_v2_GetPreviewDefinitionsResponse, GetPreviewDefinitionsResponse__Output as _superverse_v2_GetPreviewDefinitionsResponse__Output } from './superverse/v2/GetPreviewDefinitionsResponse';
import type { GetPreviewRequest as _superverse_v2_GetPreviewRequest, GetPreviewRequest__Output as _superverse_v2_GetPreviewRequest__Output } from './superverse/v2/GetPreviewRequest';
import type { GetPreviewResponse as _superverse_v2_GetPreviewResponse, GetPreviewResponse__Output as _superverse_v2_GetPreviewResponse__Output } from './superverse/v2/GetPreviewResponse';
import type { GetProjectConfigurationRequest as _superverse_v2_GetProjectConfigurationRequest, GetProjectConfigurationRequest__Output as _superverse_v2_GetProjectConfigurationRequest__Output } from './superverse/v2/GetProjectConfigurationRequest';
import type { GetProjectConfigurationResponse as _superverse_v2_GetProjectConfigurationResponse, GetProjectConfigurationResponse__Output as _superverse_v2_GetProjectConfigurationResponse__Output } from './superverse/v2/GetProjectConfigurationResponse';
import type { GetProjectRequest as _superverse_v2_GetProjectRequest, GetProjectRequest__Output as _superverse_v2_GetProjectRequest__Output } from './superverse/v2/GetProjectRequest';
import type { GetProjectResponse as _superverse_v2_GetProjectResponse, GetProjectResponse__Output as _superverse_v2_GetProjectResponse__Output } from './superverse/v2/GetProjectResponse';
import type { GetReleaseBuildLogRequest as _superverse_v2_GetReleaseBuildLogRequest, GetReleaseBuildLogRequest__Output as _superverse_v2_GetReleaseBuildLogRequest__Output } from './superverse/v2/GetReleaseBuildLogRequest';
import type { GetReleaseBuildLogResponse as _superverse_v2_GetReleaseBuildLogResponse, GetReleaseBuildLogResponse__Output as _superverse_v2_GetReleaseBuildLogResponse__Output } from './superverse/v2/GetReleaseBuildLogResponse';
import type { GetReleaseRequest as _superverse_v2_GetReleaseRequest, GetReleaseRequest__Output as _superverse_v2_GetReleaseRequest__Output } from './superverse/v2/GetReleaseRequest';
import type { GetReleaseResponse as _superverse_v2_GetReleaseResponse, GetReleaseResponse__Output as _superverse_v2_GetReleaseResponse__Output } from './superverse/v2/GetReleaseResponse';
import type { GetSessionRequest as _superverse_v2_GetSessionRequest, GetSessionRequest__Output as _superverse_v2_GetSessionRequest__Output } from './superverse/v2/GetSessionRequest';
import type { GetSessionResponse as _superverse_v2_GetSessionResponse, GetSessionResponse__Output as _superverse_v2_GetSessionResponse__Output } from './superverse/v2/GetSessionResponse';
import type { GetTeamSpaceConnectionRequest as _superverse_v2_GetTeamSpaceConnectionRequest, GetTeamSpaceConnectionRequest__Output as _superverse_v2_GetTeamSpaceConnectionRequest__Output } from './superverse/v2/GetTeamSpaceConnectionRequest';
import type { GetTeamSpaceConnectionResponse as _superverse_v2_GetTeamSpaceConnectionResponse, GetTeamSpaceConnectionResponse__Output as _superverse_v2_GetTeamSpaceConnectionResponse__Output } from './superverse/v2/GetTeamSpaceConnectionResponse';
import type { GetTeamSpaceOverviewRequest as _superverse_v2_GetTeamSpaceOverviewRequest, GetTeamSpaceOverviewRequest__Output as _superverse_v2_GetTeamSpaceOverviewRequest__Output } from './superverse/v2/GetTeamSpaceOverviewRequest';
import type { GetTeamSpaceOverviewResponse as _superverse_v2_GetTeamSpaceOverviewResponse, GetTeamSpaceOverviewResponse__Output as _superverse_v2_GetTeamSpaceOverviewResponse__Output } from './superverse/v2/GetTeamSpaceOverviewResponse';
import type { GetTeamSpaceRequest as _superverse_v2_GetTeamSpaceRequest, GetTeamSpaceRequest__Output as _superverse_v2_GetTeamSpaceRequest__Output } from './superverse/v2/GetTeamSpaceRequest';
import type { GetTeamSpaceResponse as _superverse_v2_GetTeamSpaceResponse, GetTeamSpaceResponse__Output as _superverse_v2_GetTeamSpaceResponse__Output } from './superverse/v2/GetTeamSpaceResponse';
import type { InterruptAgentRequest as _superverse_v2_InterruptAgentRequest, InterruptAgentRequest__Output as _superverse_v2_InterruptAgentRequest__Output } from './superverse/v2/InterruptAgentRequest';
import type { InterruptAgentResponse as _superverse_v2_InterruptAgentResponse, InterruptAgentResponse__Output as _superverse_v2_InterruptAgentResponse__Output } from './superverse/v2/InterruptAgentResponse';
import type { InvokeFlowActionRequest as _superverse_v2_InvokeFlowActionRequest, InvokeFlowActionRequest__Output as _superverse_v2_InvokeFlowActionRequest__Output } from './superverse/v2/InvokeFlowActionRequest';
import type { InvokeFlowActionResponse as _superverse_v2_InvokeFlowActionResponse, InvokeFlowActionResponse__Output as _superverse_v2_InvokeFlowActionResponse__Output } from './superverse/v2/InvokeFlowActionResponse';
import type { InvokeProviderCommandRequest as _superverse_v2_InvokeProviderCommandRequest, InvokeProviderCommandRequest__Output as _superverse_v2_InvokeProviderCommandRequest__Output } from './superverse/v2/InvokeProviderCommandRequest';
import type { InvokeProviderCommandResponse as _superverse_v2_InvokeProviderCommandResponse, InvokeProviderCommandResponse__Output as _superverse_v2_InvokeProviderCommandResponse__Output } from './superverse/v2/InvokeProviderCommandResponse';
import type { InvokeTeamSpaceConnectionCommandRequest as _superverse_v2_InvokeTeamSpaceConnectionCommandRequest, InvokeTeamSpaceConnectionCommandRequest__Output as _superverse_v2_InvokeTeamSpaceConnectionCommandRequest__Output } from './superverse/v2/InvokeTeamSpaceConnectionCommandRequest';
import type { InvokeTeamSpaceConnectionCommandResponse as _superverse_v2_InvokeTeamSpaceConnectionCommandResponse, InvokeTeamSpaceConnectionCommandResponse__Output as _superverse_v2_InvokeTeamSpaceConnectionCommandResponse__Output } from './superverse/v2/InvokeTeamSpaceConnectionCommandResponse';
import type { ListAgentModelsRequest as _superverse_v2_ListAgentModelsRequest, ListAgentModelsRequest__Output as _superverse_v2_ListAgentModelsRequest__Output } from './superverse/v2/ListAgentModelsRequest';
import type { ListAgentModelsResponse as _superverse_v2_ListAgentModelsResponse, ListAgentModelsResponse__Output as _superverse_v2_ListAgentModelsResponse__Output } from './superverse/v2/ListAgentModelsResponse';
import type { ListArchivedMessagesRequest as _superverse_v2_ListArchivedMessagesRequest, ListArchivedMessagesRequest__Output as _superverse_v2_ListArchivedMessagesRequest__Output } from './superverse/v2/ListArchivedMessagesRequest';
import type { ListArchivedMessagesResponse as _superverse_v2_ListArchivedMessagesResponse, ListArchivedMessagesResponse__Output as _superverse_v2_ListArchivedMessagesResponse__Output } from './superverse/v2/ListArchivedMessagesResponse';
import type { ListConnectionsRequest as _superverse_v2_ListConnectionsRequest, ListConnectionsRequest__Output as _superverse_v2_ListConnectionsRequest__Output } from './superverse/v2/ListConnectionsRequest';
import type { ListConnectionsResponse as _superverse_v2_ListConnectionsResponse, ListConnectionsResponse__Output as _superverse_v2_ListConnectionsResponse__Output } from './superverse/v2/ListConnectionsResponse';
import type { ListConnectorCatalogRequest as _superverse_v2_ListConnectorCatalogRequest, ListConnectorCatalogRequest__Output as _superverse_v2_ListConnectorCatalogRequest__Output } from './superverse/v2/ListConnectorCatalogRequest';
import type { ListConnectorCatalogResponse as _superverse_v2_ListConnectorCatalogResponse, ListConnectorCatalogResponse__Output as _superverse_v2_ListConnectorCatalogResponse__Output } from './superverse/v2/ListConnectorCatalogResponse';
import type { ListDeploymentsRequest as _superverse_v2_ListDeploymentsRequest, ListDeploymentsRequest__Output as _superverse_v2_ListDeploymentsRequest__Output } from './superverse/v2/ListDeploymentsRequest';
import type { ListDeploymentsResponse as _superverse_v2_ListDeploymentsResponse, ListDeploymentsResponse__Output as _superverse_v2_ListDeploymentsResponse__Output } from './superverse/v2/ListDeploymentsResponse';
import type { ListEnvironmentEventsRequest as _superverse_v2_ListEnvironmentEventsRequest, ListEnvironmentEventsRequest__Output as _superverse_v2_ListEnvironmentEventsRequest__Output } from './superverse/v2/ListEnvironmentEventsRequest';
import type { ListEnvironmentEventsResponse as _superverse_v2_ListEnvironmentEventsResponse, ListEnvironmentEventsResponse__Output as _superverse_v2_ListEnvironmentEventsResponse__Output } from './superverse/v2/ListEnvironmentEventsResponse';
import type { ListMockScenariosRequest as _superverse_v2_ListMockScenariosRequest, ListMockScenariosRequest__Output as _superverse_v2_ListMockScenariosRequest__Output } from './superverse/v2/ListMockScenariosRequest';
import type { ListMockScenariosResponse as _superverse_v2_ListMockScenariosResponse, ListMockScenariosResponse__Output as _superverse_v2_ListMockScenariosResponse__Output } from './superverse/v2/ListMockScenariosResponse';
import type { ListModelCredentialModelsRequest as _superverse_v2_ListModelCredentialModelsRequest, ListModelCredentialModelsRequest__Output as _superverse_v2_ListModelCredentialModelsRequest__Output } from './superverse/v2/ListModelCredentialModelsRequest';
import type { ListModelCredentialModelsResponse as _superverse_v2_ListModelCredentialModelsResponse, ListModelCredentialModelsResponse__Output as _superverse_v2_ListModelCredentialModelsResponse__Output } from './superverse/v2/ListModelCredentialModelsResponse';
import type { ListProjectMembersRequest as _superverse_v2_ListProjectMembersRequest, ListProjectMembersRequest__Output as _superverse_v2_ListProjectMembersRequest__Output } from './superverse/v2/ListProjectMembersRequest';
import type { ListProjectMembersResponse as _superverse_v2_ListProjectMembersResponse, ListProjectMembersResponse__Output as _superverse_v2_ListProjectMembersResponse__Output } from './superverse/v2/ListProjectMembersResponse';
import type { ListProjectsRequest as _superverse_v2_ListProjectsRequest, ListProjectsRequest__Output as _superverse_v2_ListProjectsRequest__Output } from './superverse/v2/ListProjectsRequest';
import type { ListProjectsResponse as _superverse_v2_ListProjectsResponse, ListProjectsResponse__Output as _superverse_v2_ListProjectsResponse__Output } from './superverse/v2/ListProjectsResponse';
import type { ListRecentTriggerEventsRequest as _superverse_v2_ListRecentTriggerEventsRequest, ListRecentTriggerEventsRequest__Output as _superverse_v2_ListRecentTriggerEventsRequest__Output } from './superverse/v2/ListRecentTriggerEventsRequest';
import type { ListRecentTriggerEventsResponse as _superverse_v2_ListRecentTriggerEventsResponse, ListRecentTriggerEventsResponse__Output as _superverse_v2_ListRecentTriggerEventsResponse__Output } from './superverse/v2/ListRecentTriggerEventsResponse';
import type { ListReleaseCandidatesRequest as _superverse_v2_ListReleaseCandidatesRequest, ListReleaseCandidatesRequest__Output as _superverse_v2_ListReleaseCandidatesRequest__Output } from './superverse/v2/ListReleaseCandidatesRequest';
import type { ListReleaseCandidatesResponse as _superverse_v2_ListReleaseCandidatesResponse, ListReleaseCandidatesResponse__Output as _superverse_v2_ListReleaseCandidatesResponse__Output } from './superverse/v2/ListReleaseCandidatesResponse';
import type { ListReleasesRequest as _superverse_v2_ListReleasesRequest, ListReleasesRequest__Output as _superverse_v2_ListReleasesRequest__Output } from './superverse/v2/ListReleasesRequest';
import type { ListReleasesResponse as _superverse_v2_ListReleasesResponse, ListReleasesResponse__Output as _superverse_v2_ListReleasesResponse__Output } from './superverse/v2/ListReleasesResponse';
import type { ListRemoteAgentConnectionsRequest as _superverse_v2_ListRemoteAgentConnectionsRequest, ListRemoteAgentConnectionsRequest__Output as _superverse_v2_ListRemoteAgentConnectionsRequest__Output } from './superverse/v2/ListRemoteAgentConnectionsRequest';
import type { ListRemoteAgentConnectionsResponse as _superverse_v2_ListRemoteAgentConnectionsResponse, ListRemoteAgentConnectionsResponse__Output as _superverse_v2_ListRemoteAgentConnectionsResponse__Output } from './superverse/v2/ListRemoteAgentConnectionsResponse';
import type { ListTeamSpaceConnectionsRequest as _superverse_v2_ListTeamSpaceConnectionsRequest, ListTeamSpaceConnectionsRequest__Output as _superverse_v2_ListTeamSpaceConnectionsRequest__Output } from './superverse/v2/ListTeamSpaceConnectionsRequest';
import type { ListTeamSpaceConnectionsResponse as _superverse_v2_ListTeamSpaceConnectionsResponse, ListTeamSpaceConnectionsResponse__Output as _superverse_v2_ListTeamSpaceConnectionsResponse__Output } from './superverse/v2/ListTeamSpaceConnectionsResponse';
import type { ListTeamSpaceMembersRequest as _superverse_v2_ListTeamSpaceMembersRequest, ListTeamSpaceMembersRequest__Output as _superverse_v2_ListTeamSpaceMembersRequest__Output } from './superverse/v2/ListTeamSpaceMembersRequest';
import type { ListTeamSpaceMembersResponse as _superverse_v2_ListTeamSpaceMembersResponse, ListTeamSpaceMembersResponse__Output as _superverse_v2_ListTeamSpaceMembersResponse__Output } from './superverse/v2/ListTeamSpaceMembersResponse';
import type { ListTeamSpacesRequest as _superverse_v2_ListTeamSpacesRequest, ListTeamSpacesRequest__Output as _superverse_v2_ListTeamSpacesRequest__Output } from './superverse/v2/ListTeamSpacesRequest';
import type { ListTeamSpacesResponse as _superverse_v2_ListTeamSpacesResponse, ListTeamSpacesResponse__Output as _superverse_v2_ListTeamSpacesResponse__Output } from './superverse/v2/ListTeamSpacesResponse';
import type { LoadFlowBlobsRequest as _superverse_v2_LoadFlowBlobsRequest, LoadFlowBlobsRequest__Output as _superverse_v2_LoadFlowBlobsRequest__Output } from './superverse/v2/LoadFlowBlobsRequest';
import type { LoadFlowBlobsResponse as _superverse_v2_LoadFlowBlobsResponse, LoadFlowBlobsResponse__Output as _superverse_v2_LoadFlowBlobsResponse__Output } from './superverse/v2/LoadFlowBlobsResponse';
import type { LoginRequest as _superverse_v2_LoginRequest, LoginRequest__Output as _superverse_v2_LoginRequest__Output } from './superverse/v2/LoginRequest';
import type { LoginResponse as _superverse_v2_LoginResponse, LoginResponse__Output as _superverse_v2_LoginResponse__Output } from './superverse/v2/LoginResponse';
import type { LogoutRequest as _superverse_v2_LogoutRequest, LogoutRequest__Output as _superverse_v2_LogoutRequest__Output } from './superverse/v2/LogoutRequest';
import type { LogoutResponse as _superverse_v2_LogoutResponse, LogoutResponse__Output as _superverse_v2_LogoutResponse__Output } from './superverse/v2/LogoutResponse';
import type { ManagedRepositorySource as _superverse_v2_ManagedRepositorySource, ManagedRepositorySource__Output as _superverse_v2_ManagedRepositorySource__Output } from './superverse/v2/ManagedRepositorySource';
import type { ModelCredentialInfo as _superverse_v2_ModelCredentialInfo, ModelCredentialInfo__Output as _superverse_v2_ModelCredentialInfo__Output } from './superverse/v2/ModelCredentialInfo';
import type { ModelCredentialServiceClient as _superverse_v2_ModelCredentialServiceClient, ModelCredentialServiceDefinition as _superverse_v2_ModelCredentialServiceDefinition } from './superverse/v2/ModelCredentialService';
import type { NewTeamSpaceConnection as _superverse_v2_NewTeamSpaceConnection, NewTeamSpaceConnection__Output as _superverse_v2_NewTeamSpaceConnection__Output } from './superverse/v2/NewTeamSpaceConnection';
import type { ObjectReference as _superverse_v2_ObjectReference, ObjectReference__Output as _superverse_v2_ObjectReference__Output } from './superverse/v2/ObjectReference';
import type { PhaseTiming as _superverse_v2_PhaseTiming, PhaseTiming__Output as _superverse_v2_PhaseTiming__Output } from './superverse/v2/PhaseTiming';
import type { PreparePreviewSourceRequest as _superverse_v2_PreparePreviewSourceRequest, PreparePreviewSourceRequest__Output as _superverse_v2_PreparePreviewSourceRequest__Output } from './superverse/v2/PreparePreviewSourceRequest';
import type { PreparePreviewSourceResponse as _superverse_v2_PreparePreviewSourceResponse, PreparePreviewSourceResponse__Output as _superverse_v2_PreparePreviewSourceResponse__Output } from './superverse/v2/PreparePreviewSourceResponse';
import type { PrepareReleaseRequest as _superverse_v2_PrepareReleaseRequest, PrepareReleaseRequest__Output as _superverse_v2_PrepareReleaseRequest__Output } from './superverse/v2/PrepareReleaseRequest';
import type { PrepareReleaseResponse as _superverse_v2_PrepareReleaseResponse, PrepareReleaseResponse__Output as _superverse_v2_PrepareReleaseResponse__Output } from './superverse/v2/PrepareReleaseResponse';
import type { PreparedPreviewSourceSnapshot as _superverse_v2_PreparedPreviewSourceSnapshot, PreparedPreviewSourceSnapshot__Output as _superverse_v2_PreparedPreviewSourceSnapshot__Output } from './superverse/v2/PreparedPreviewSourceSnapshot';
import type { PreparedReleaseSourceSnapshot as _superverse_v2_PreparedReleaseSourceSnapshot, PreparedReleaseSourceSnapshot__Output as _superverse_v2_PreparedReleaseSourceSnapshot__Output } from './superverse/v2/PreparedReleaseSourceSnapshot';
import type { PreviewDeploymentSnapshot as _superverse_v2_PreviewDeploymentSnapshot, PreviewDeploymentSnapshot__Output as _superverse_v2_PreviewDeploymentSnapshot__Output } from './superverse/v2/PreviewDeploymentSnapshot';
import type { PreviewServiceClient as _superverse_v2_PreviewServiceClient, PreviewServiceDefinition as _superverse_v2_PreviewServiceDefinition } from './superverse/v2/PreviewService';
import type { PreviewSnapshot as _superverse_v2_PreviewSnapshot, PreviewSnapshot__Output as _superverse_v2_PreviewSnapshot__Output } from './superverse/v2/PreviewSnapshot';
import type { PreviewStartRecord as _superverse_v2_PreviewStartRecord, PreviewStartRecord__Output as _superverse_v2_PreviewStartRecord__Output } from './superverse/v2/PreviewStartRecord';
import type { PreviewValidation as _superverse_v2_PreviewValidation, PreviewValidation__Output as _superverse_v2_PreviewValidation__Output } from './superverse/v2/PreviewValidation';
import type { ProjectConfigurationSnapshot as _superverse_v2_ProjectConfigurationSnapshot, ProjectConfigurationSnapshot__Output as _superverse_v2_ProjectConfigurationSnapshot__Output } from './superverse/v2/ProjectConfigurationSnapshot';
import type { ProjectConfigurationTarget as _superverse_v2_ProjectConfigurationTarget, ProjectConfigurationTarget__Output as _superverse_v2_ProjectConfigurationTarget__Output } from './superverse/v2/ProjectConfigurationTarget';
import type { ProjectConfigurationValidation as _superverse_v2_ProjectConfigurationValidation, ProjectConfigurationValidation__Output as _superverse_v2_ProjectConfigurationValidation__Output } from './superverse/v2/ProjectConfigurationValidation';
import type { ProjectExample as _superverse_v2_ProjectExample, ProjectExample__Output as _superverse_v2_ProjectExample__Output } from './superverse/v2/ProjectExample';
import type { ProjectLive as _superverse_v2_ProjectLive, ProjectLive__Output as _superverse_v2_ProjectLive__Output } from './superverse/v2/ProjectLive';
import type { ProjectMemberServiceClient as _superverse_v2_ProjectMemberServiceClient, ProjectMemberServiceDefinition as _superverse_v2_ProjectMemberServiceDefinition } from './superverse/v2/ProjectMemberService';
import type { ProjectMemberSnapshot as _superverse_v2_ProjectMemberSnapshot, ProjectMemberSnapshot__Output as _superverse_v2_ProjectMemberSnapshot__Output } from './superverse/v2/ProjectMemberSnapshot';
import type { ProjectServiceClient as _superverse_v2_ProjectServiceClient, ProjectServiceDefinition as _superverse_v2_ProjectServiceDefinition } from './superverse/v2/ProjectService';
import type { ProjectSnapshot as _superverse_v2_ProjectSnapshot, ProjectSnapshot__Output as _superverse_v2_ProjectSnapshot__Output } from './superverse/v2/ProjectSnapshot';
import type { ReadFlowStreamRequest as _superverse_v2_ReadFlowStreamRequest, ReadFlowStreamRequest__Output as _superverse_v2_ReadFlowStreamRequest__Output } from './superverse/v2/ReadFlowStreamRequest';
import type { ReadFlowStreamResponse as _superverse_v2_ReadFlowStreamResponse, ReadFlowStreamResponse__Output as _superverse_v2_ReadFlowStreamResponse__Output } from './superverse/v2/ReadFlowStreamResponse';
import type { RecentTriggerEvent as _superverse_v2_RecentTriggerEvent, RecentTriggerEvent__Output as _superverse_v2_RecentTriggerEvent__Output } from './superverse/v2/RecentTriggerEvent';
import type { RecoverFlowStartRequest as _superverse_v2_RecoverFlowStartRequest, RecoverFlowStartRequest__Output as _superverse_v2_RecoverFlowStartRequest__Output } from './superverse/v2/RecoverFlowStartRequest';
import type { RecoverFlowStartResponse as _superverse_v2_RecoverFlowStartResponse, RecoverFlowStartResponse__Output as _superverse_v2_RecoverFlowStartResponse__Output } from './superverse/v2/RecoverFlowStartResponse';
import type { RecoverReleaseBuildRequest as _superverse_v2_RecoverReleaseBuildRequest, RecoverReleaseBuildRequest__Output as _superverse_v2_RecoverReleaseBuildRequest__Output } from './superverse/v2/RecoverReleaseBuildRequest';
import type { RecoverReleaseBuildResponse as _superverse_v2_RecoverReleaseBuildResponse, RecoverReleaseBuildResponse__Output as _superverse_v2_RecoverReleaseBuildResponse__Output } from './superverse/v2/RecoverReleaseBuildResponse';
import type { RegisterAccountRequest as _superverse_v2_RegisterAccountRequest, RegisterAccountRequest__Output as _superverse_v2_RegisterAccountRequest__Output } from './superverse/v2/RegisterAccountRequest';
import type { RegisterAccountResponse as _superverse_v2_RegisterAccountResponse, RegisterAccountResponse__Output as _superverse_v2_RegisterAccountResponse__Output } from './superverse/v2/RegisterAccountResponse';
import type { ReleaseBuildRevisionSnapshot as _superverse_v2_ReleaseBuildRevisionSnapshot, ReleaseBuildRevisionSnapshot__Output as _superverse_v2_ReleaseBuildRevisionSnapshot__Output } from './superverse/v2/ReleaseBuildRevisionSnapshot';
import type { ReleaseCandidate as _superverse_v2_ReleaseCandidate, ReleaseCandidate__Output as _superverse_v2_ReleaseCandidate__Output } from './superverse/v2/ReleaseCandidate';
import type { ReleaseServiceClient as _superverse_v2_ReleaseServiceClient, ReleaseServiceDefinition as _superverse_v2_ReleaseServiceDefinition } from './superverse/v2/ReleaseService';
import type { ReleaseSnapshot as _superverse_v2_ReleaseSnapshot, ReleaseSnapshot__Output as _superverse_v2_ReleaseSnapshot__Output } from './superverse/v2/ReleaseSnapshot';
import type { ReleaseUnusedConnectionsRequest as _superverse_v2_ReleaseUnusedConnectionsRequest, ReleaseUnusedConnectionsRequest__Output as _superverse_v2_ReleaseUnusedConnectionsRequest__Output } from './superverse/v2/ReleaseUnusedConnectionsRequest';
import type { ReleaseUnusedConnectionsResponse as _superverse_v2_ReleaseUnusedConnectionsResponse, ReleaseUnusedConnectionsResponse__Output as _superverse_v2_ReleaseUnusedConnectionsResponse__Output } from './superverse/v2/ReleaseUnusedConnectionsResponse';
import type { RemoteAgentBinding as _superverse_v2_RemoteAgentBinding, RemoteAgentBinding__Output as _superverse_v2_RemoteAgentBinding__Output } from './superverse/v2/RemoteAgentBinding';
import type { RemoteAgentConnection as _superverse_v2_RemoteAgentConnection, RemoteAgentConnection__Output as _superverse_v2_RemoteAgentConnection__Output } from './superverse/v2/RemoteAgentConnection';
import type { RemoteAgentServiceClient as _superverse_v2_RemoteAgentServiceClient, RemoteAgentServiceDefinition as _superverse_v2_RemoteAgentServiceDefinition } from './superverse/v2/RemoteAgentService';
import type { RemoteSessionExecutor as _superverse_v2_RemoteSessionExecutor, RemoteSessionExecutor__Output as _superverse_v2_RemoteSessionExecutor__Output } from './superverse/v2/RemoteSessionExecutor';
import type { RemoveLiveRequest as _superverse_v2_RemoveLiveRequest, RemoveLiveRequest__Output as _superverse_v2_RemoveLiveRequest__Output } from './superverse/v2/RemoveLiveRequest';
import type { RemoveLiveResponse as _superverse_v2_RemoveLiveResponse, RemoveLiveResponse__Output as _superverse_v2_RemoveLiveResponse__Output } from './superverse/v2/RemoveLiveResponse';
import type { RemoveProjectMemberRequest as _superverse_v2_RemoveProjectMemberRequest, RemoveProjectMemberRequest__Output as _superverse_v2_RemoveProjectMemberRequest__Output } from './superverse/v2/RemoveProjectMemberRequest';
import type { RemoveProjectMemberResponse as _superverse_v2_RemoveProjectMemberResponse, RemoveProjectMemberResponse__Output as _superverse_v2_RemoveProjectMemberResponse__Output } from './superverse/v2/RemoveProjectMemberResponse';
import type { RemoveTeamSpaceMemberRequest as _superverse_v2_RemoveTeamSpaceMemberRequest, RemoveTeamSpaceMemberRequest__Output as _superverse_v2_RemoveTeamSpaceMemberRequest__Output } from './superverse/v2/RemoveTeamSpaceMemberRequest';
import type { RemoveTeamSpaceMemberResponse as _superverse_v2_RemoveTeamSpaceMemberResponse, RemoveTeamSpaceMemberResponse__Output as _superverse_v2_RemoveTeamSpaceMemberResponse__Output } from './superverse/v2/RemoveTeamSpaceMemberResponse';
import type { RenameTeamSpaceRequest as _superverse_v2_RenameTeamSpaceRequest, RenameTeamSpaceRequest__Output as _superverse_v2_RenameTeamSpaceRequest__Output } from './superverse/v2/RenameTeamSpaceRequest';
import type { RenameTeamSpaceResponse as _superverse_v2_RenameTeamSpaceResponse, RenameTeamSpaceResponse__Output as _superverse_v2_RenameTeamSpaceResponse__Output } from './superverse/v2/RenameTeamSpaceResponse';
import type { RenewWorkspaceRequest as _superverse_v2_RenewWorkspaceRequest, RenewWorkspaceRequest__Output as _superverse_v2_RenewWorkspaceRequest__Output } from './superverse/v2/RenewWorkspaceRequest';
import type { RenewWorkspaceResponse as _superverse_v2_RenewWorkspaceResponse, RenewWorkspaceResponse__Output as _superverse_v2_RenewWorkspaceResponse__Output } from './superverse/v2/RenewWorkspaceResponse';
import type { Repository as _superverse_v2_Repository, Repository__Output as _superverse_v2_Repository__Output } from './superverse/v2/Repository';
import type { ResolveToolApprovalRequest as _superverse_v2_ResolveToolApprovalRequest, ResolveToolApprovalRequest__Output as _superverse_v2_ResolveToolApprovalRequest__Output } from './superverse/v2/ResolveToolApprovalRequest';
import type { ResolveToolApprovalResponse as _superverse_v2_ResolveToolApprovalResponse, ResolveToolApprovalResponse__Output as _superverse_v2_ResolveToolApprovalResponse__Output } from './superverse/v2/ResolveToolApprovalResponse';
import type { ResourceProgress as _superverse_v2_ResourceProgress, ResourceProgress__Output as _superverse_v2_ResourceProgress__Output } from './superverse/v2/ResourceProgress';
import type { RevokeRemoteAgentConnectionRequest as _superverse_v2_RevokeRemoteAgentConnectionRequest, RevokeRemoteAgentConnectionRequest__Output as _superverse_v2_RevokeRemoteAgentConnectionRequest__Output } from './superverse/v2/RevokeRemoteAgentConnectionRequest';
import type { RevokeRemoteAgentConnectionResponse as _superverse_v2_RevokeRemoteAgentConnectionResponse, RevokeRemoteAgentConnectionResponse__Output as _superverse_v2_RevokeRemoteAgentConnectionResponse__Output } from './superverse/v2/RevokeRemoteAgentConnectionResponse';
import type { SaveApplicationEnvironmentRequest as _superverse_v2_SaveApplicationEnvironmentRequest, SaveApplicationEnvironmentRequest__Output as _superverse_v2_SaveApplicationEnvironmentRequest__Output } from './superverse/v2/SaveApplicationEnvironmentRequest';
import type { SaveApplicationEnvironmentResponse as _superverse_v2_SaveApplicationEnvironmentResponse, SaveApplicationEnvironmentResponse__Output as _superverse_v2_SaveApplicationEnvironmentResponse__Output } from './superverse/v2/SaveApplicationEnvironmentResponse';
import type { SaveOperationConfigurationRequest as _superverse_v2_SaveOperationConfigurationRequest, SaveOperationConfigurationRequest__Output as _superverse_v2_SaveOperationConfigurationRequest__Output } from './superverse/v2/SaveOperationConfigurationRequest';
import type { SaveOperationConfigurationResponse as _superverse_v2_SaveOperationConfigurationResponse, SaveOperationConfigurationResponse__Output as _superverse_v2_SaveOperationConfigurationResponse__Output } from './superverse/v2/SaveOperationConfigurationResponse';
import type { SaveTriggerBindingRequest as _superverse_v2_SaveTriggerBindingRequest, SaveTriggerBindingRequest__Output as _superverse_v2_SaveTriggerBindingRequest__Output } from './superverse/v2/SaveTriggerBindingRequest';
import type { SaveTriggerBindingResponse as _superverse_v2_SaveTriggerBindingResponse, SaveTriggerBindingResponse__Output as _superverse_v2_SaveTriggerBindingResponse__Output } from './superverse/v2/SaveTriggerBindingResponse';
import type { SearchFlowsRequest as _superverse_v2_SearchFlowsRequest, SearchFlowsRequest__Output as _superverse_v2_SearchFlowsRequest__Output } from './superverse/v2/SearchFlowsRequest';
import type { SearchFlowsResponse as _superverse_v2_SearchFlowsResponse, SearchFlowsResponse__Output as _superverse_v2_SearchFlowsResponse__Output } from './superverse/v2/SearchFlowsResponse';
import type { SelectTeamSpaceConnectionRequest as _superverse_v2_SelectTeamSpaceConnectionRequest, SelectTeamSpaceConnectionRequest__Output as _superverse_v2_SelectTeamSpaceConnectionRequest__Output } from './superverse/v2/SelectTeamSpaceConnectionRequest';
import type { SelectTeamSpaceConnectionResponse as _superverse_v2_SelectTeamSpaceConnectionResponse, SelectTeamSpaceConnectionResponse__Output as _superverse_v2_SelectTeamSpaceConnectionResponse__Output } from './superverse/v2/SelectTeamSpaceConnectionResponse';
import type { SendMessageRequest as _superverse_v2_SendMessageRequest, SendMessageRequest__Output as _superverse_v2_SendMessageRequest__Output } from './superverse/v2/SendMessageRequest';
import type { SendMessageResponse as _superverse_v2_SendMessageResponse, SendMessageResponse__Output as _superverse_v2_SendMessageResponse__Output } from './superverse/v2/SendMessageResponse';
import type { SessionExecutor as _superverse_v2_SessionExecutor, SessionExecutor__Output as _superverse_v2_SessionExecutor__Output } from './superverse/v2/SessionExecutor';
import type { SessionProgress as _superverse_v2_SessionProgress, SessionProgress__Output as _superverse_v2_SessionProgress__Output } from './superverse/v2/SessionProgress';
import type { SessionServiceClient as _superverse_v2_SessionServiceClient, SessionServiceDefinition as _superverse_v2_SessionServiceDefinition } from './superverse/v2/SessionService';
import type { SessionSnapshot as _superverse_v2_SessionSnapshot, SessionSnapshot__Output as _superverse_v2_SessionSnapshot__Output } from './superverse/v2/SessionSnapshot';
import type { SessionWorkspaceSnapshot as _superverse_v2_SessionWorkspaceSnapshot, SessionWorkspaceSnapshot__Output as _superverse_v2_SessionWorkspaceSnapshot__Output } from './superverse/v2/SessionWorkspaceSnapshot';
import type { SetModelCredentialRequest as _superverse_v2_SetModelCredentialRequest, SetModelCredentialRequest__Output as _superverse_v2_SetModelCredentialRequest__Output } from './superverse/v2/SetModelCredentialRequest';
import type { SetModelCredentialResponse as _superverse_v2_SetModelCredentialResponse, SetModelCredentialResponse__Output as _superverse_v2_SetModelCredentialResponse__Output } from './superverse/v2/SetModelCredentialResponse';
import type { SetProjectMemberRoleRequest as _superverse_v2_SetProjectMemberRoleRequest, SetProjectMemberRoleRequest__Output as _superverse_v2_SetProjectMemberRoleRequest__Output } from './superverse/v2/SetProjectMemberRoleRequest';
import type { SetProjectMemberRoleResponse as _superverse_v2_SetProjectMemberRoleResponse, SetProjectMemberRoleResponse__Output as _superverse_v2_SetProjectMemberRoleResponse__Output } from './superverse/v2/SetProjectMemberRoleResponse';
import type { SetSessionExecutorRequest as _superverse_v2_SetSessionExecutorRequest, SetSessionExecutorRequest__Output as _superverse_v2_SetSessionExecutorRequest__Output } from './superverse/v2/SetSessionExecutorRequest';
import type { SetSessionExecutorResponse as _superverse_v2_SetSessionExecutorResponse, SetSessionExecutorResponse__Output as _superverse_v2_SetSessionExecutorResponse__Output } from './superverse/v2/SetSessionExecutorResponse';
import type { SetTeamSpaceMemberRoleRequest as _superverse_v2_SetTeamSpaceMemberRoleRequest, SetTeamSpaceMemberRoleRequest__Output as _superverse_v2_SetTeamSpaceMemberRoleRequest__Output } from './superverse/v2/SetTeamSpaceMemberRoleRequest';
import type { SetTeamSpaceMemberRoleResponse as _superverse_v2_SetTeamSpaceMemberRoleResponse, SetTeamSpaceMemberRoleResponse__Output as _superverse_v2_SetTeamSpaceMemberRoleResponse__Output } from './superverse/v2/SetTeamSpaceMemberRoleResponse';
import type { SetupSnapshot as _superverse_v2_SetupSnapshot, SetupSnapshot__Output as _superverse_v2_SetupSnapshot__Output } from './superverse/v2/SetupSnapshot';
import type { StartConnectorUiSessionRequest as _superverse_v2_StartConnectorUiSessionRequest, StartConnectorUiSessionRequest__Output as _superverse_v2_StartConnectorUiSessionRequest__Output } from './superverse/v2/StartConnectorUiSessionRequest';
import type { StartConnectorUiSessionResponse as _superverse_v2_StartConnectorUiSessionResponse, StartConnectorUiSessionResponse__Output as _superverse_v2_StartConnectorUiSessionResponse__Output } from './superverse/v2/StartConnectorUiSessionResponse';
import type { StartDeploymentRequest as _superverse_v2_StartDeploymentRequest, StartDeploymentRequest__Output as _superverse_v2_StartDeploymentRequest__Output } from './superverse/v2/StartDeploymentRequest';
import type { StartDeploymentResponse as _superverse_v2_StartDeploymentResponse, StartDeploymentResponse__Output as _superverse_v2_StartDeploymentResponse__Output } from './superverse/v2/StartDeploymentResponse';
import type { StartEvolveReviewRequest as _superverse_v2_StartEvolveReviewRequest, StartEvolveReviewRequest__Output as _superverse_v2_StartEvolveReviewRequest__Output } from './superverse/v2/StartEvolveReviewRequest';
import type { StartEvolveReviewResponse as _superverse_v2_StartEvolveReviewResponse, StartEvolveReviewResponse__Output as _superverse_v2_StartEvolveReviewResponse__Output } from './superverse/v2/StartEvolveReviewResponse';
import type { StartFlowRequest as _superverse_v2_StartFlowRequest, StartFlowRequest__Output as _superverse_v2_StartFlowRequest__Output } from './superverse/v2/StartFlowRequest';
import type { StartFlowResponse as _superverse_v2_StartFlowResponse, StartFlowResponse__Output as _superverse_v2_StartFlowResponse__Output } from './superverse/v2/StartFlowResponse';
import type { StartPreviewRequest as _superverse_v2_StartPreviewRequest, StartPreviewRequest__Output as _superverse_v2_StartPreviewRequest__Output } from './superverse/v2/StartPreviewRequest';
import type { StartPreviewResponse as _superverse_v2_StartPreviewResponse, StartPreviewResponse__Output as _superverse_v2_StartPreviewResponse__Output } from './superverse/v2/StartPreviewResponse';
import type { StartReleaseBuildRequest as _superverse_v2_StartReleaseBuildRequest, StartReleaseBuildRequest__Output as _superverse_v2_StartReleaseBuildRequest__Output } from './superverse/v2/StartReleaseBuildRequest';
import type { StartReleaseBuildResponse as _superverse_v2_StartReleaseBuildResponse, StartReleaseBuildResponse__Output as _superverse_v2_StartReleaseBuildResponse__Output } from './superverse/v2/StartReleaseBuildResponse';
import type { StartTeamSpaceConnectionOAuthRequest as _superverse_v2_StartTeamSpaceConnectionOAuthRequest, StartTeamSpaceConnectionOAuthRequest__Output as _superverse_v2_StartTeamSpaceConnectionOAuthRequest__Output } from './superverse/v2/StartTeamSpaceConnectionOAuthRequest';
import type { StartTeamSpaceConnectionOAuthResponse as _superverse_v2_StartTeamSpaceConnectionOAuthResponse, StartTeamSpaceConnectionOAuthResponse__Output as _superverse_v2_StartTeamSpaceConnectionOAuthResponse__Output } from './superverse/v2/StartTeamSpaceConnectionOAuthResponse';
import type { StartTeamSpaceConnectionUiSessionRequest as _superverse_v2_StartTeamSpaceConnectionUiSessionRequest, StartTeamSpaceConnectionUiSessionRequest__Output as _superverse_v2_StartTeamSpaceConnectionUiSessionRequest__Output } from './superverse/v2/StartTeamSpaceConnectionUiSessionRequest';
import type { StartTeamSpaceConnectionUiSessionResponse as _superverse_v2_StartTeamSpaceConnectionUiSessionResponse, StartTeamSpaceConnectionUiSessionResponse__Output as _superverse_v2_StartTeamSpaceConnectionUiSessionResponse__Output } from './superverse/v2/StartTeamSpaceConnectionUiSessionResponse';
import type { SteerQueuedMessageRequest as _superverse_v2_SteerQueuedMessageRequest, SteerQueuedMessageRequest__Output as _superverse_v2_SteerQueuedMessageRequest__Output } from './superverse/v2/SteerQueuedMessageRequest';
import type { SteerQueuedMessageResponse as _superverse_v2_SteerQueuedMessageResponse, SteerQueuedMessageResponse__Output as _superverse_v2_SteerQueuedMessageResponse__Output } from './superverse/v2/SteerQueuedMessageResponse';
import type { StopFlowRequest as _superverse_v2_StopFlowRequest, StopFlowRequest__Output as _superverse_v2_StopFlowRequest__Output } from './superverse/v2/StopFlowRequest';
import type { StopFlowResponse as _superverse_v2_StopFlowResponse, StopFlowResponse__Output as _superverse_v2_StopFlowResponse__Output } from './superverse/v2/StopFlowResponse';
import type { StopPreviewRequest as _superverse_v2_StopPreviewRequest, StopPreviewRequest__Output as _superverse_v2_StopPreviewRequest__Output } from './superverse/v2/StopPreviewRequest';
import type { StopPreviewResponse as _superverse_v2_StopPreviewResponse, StopPreviewResponse__Output as _superverse_v2_StopPreviewResponse__Output } from './superverse/v2/StopPreviewResponse';
import type { TeamSpaceConnectionServiceClient as _superverse_v2_TeamSpaceConnectionServiceClient, TeamSpaceConnectionServiceDefinition as _superverse_v2_TeamSpaceConnectionServiceDefinition } from './superverse/v2/TeamSpaceConnectionService';
import type { TeamSpaceConnectionSnapshot as _superverse_v2_TeamSpaceConnectionSnapshot, TeamSpaceConnectionSnapshot__Output as _superverse_v2_TeamSpaceConnectionSnapshot__Output } from './superverse/v2/TeamSpaceConnectionSnapshot';
import type { TeamSpaceConnectionUsage as _superverse_v2_TeamSpaceConnectionUsage, TeamSpaceConnectionUsage__Output as _superverse_v2_TeamSpaceConnectionUsage__Output } from './superverse/v2/TeamSpaceConnectionUsage';
import type { TeamSpaceMemberServiceClient as _superverse_v2_TeamSpaceMemberServiceClient, TeamSpaceMemberServiceDefinition as _superverse_v2_TeamSpaceMemberServiceDefinition } from './superverse/v2/TeamSpaceMemberService';
import type { TeamSpaceMemberSnapshot as _superverse_v2_TeamSpaceMemberSnapshot, TeamSpaceMemberSnapshot__Output as _superverse_v2_TeamSpaceMemberSnapshot__Output } from './superverse/v2/TeamSpaceMemberSnapshot';
import type { TeamSpaceOverview as _superverse_v2_TeamSpaceOverview, TeamSpaceOverview__Output as _superverse_v2_TeamSpaceOverview__Output } from './superverse/v2/TeamSpaceOverview';
import type { TeamSpaceServiceClient as _superverse_v2_TeamSpaceServiceClient, TeamSpaceServiceDefinition as _superverse_v2_TeamSpaceServiceDefinition } from './superverse/v2/TeamSpaceService';
import type { TeamSpaceSnapshot as _superverse_v2_TeamSpaceSnapshot, TeamSpaceSnapshot__Output as _superverse_v2_TeamSpaceSnapshot__Output } from './superverse/v2/TeamSpaceSnapshot';
import type { TimeTravelFlowRequest as _superverse_v2_TimeTravelFlowRequest, TimeTravelFlowRequest__Output as _superverse_v2_TimeTravelFlowRequest__Output } from './superverse/v2/TimeTravelFlowRequest';
import type { TimeTravelFlowResponse as _superverse_v2_TimeTravelFlowResponse, TimeTravelFlowResponse__Output as _superverse_v2_TimeTravelFlowResponse__Output } from './superverse/v2/TimeTravelFlowResponse';
import type { UnroutedEvents as _superverse_v2_UnroutedEvents, UnroutedEvents__Output as _superverse_v2_UnroutedEvents__Output } from './superverse/v2/UnroutedEvents';
import type { UpdateFlowDisplayFieldRequest as _superverse_v2_UpdateFlowDisplayFieldRequest, UpdateFlowDisplayFieldRequest__Output as _superverse_v2_UpdateFlowDisplayFieldRequest__Output } from './superverse/v2/UpdateFlowDisplayFieldRequest';
import type { UpdateFlowDisplayFieldResponse as _superverse_v2_UpdateFlowDisplayFieldResponse, UpdateFlowDisplayFieldResponse__Output as _superverse_v2_UpdateFlowDisplayFieldResponse__Output } from './superverse/v2/UpdateFlowDisplayFieldResponse';
import type { UpdatePreviewAutoDeployRequest as _superverse_v2_UpdatePreviewAutoDeployRequest, UpdatePreviewAutoDeployRequest__Output as _superverse_v2_UpdatePreviewAutoDeployRequest__Output } from './superverse/v2/UpdatePreviewAutoDeployRequest';
import type { UpdatePreviewAutoDeployResponse as _superverse_v2_UpdatePreviewAutoDeployResponse, UpdatePreviewAutoDeployResponse__Output as _superverse_v2_UpdatePreviewAutoDeployResponse__Output } from './superverse/v2/UpdatePreviewAutoDeployResponse';
import type { UpdateSessionModelRequest as _superverse_v2_UpdateSessionModelRequest, UpdateSessionModelRequest__Output as _superverse_v2_UpdateSessionModelRequest__Output } from './superverse/v2/UpdateSessionModelRequest';
import type { UpdateSessionModelResponse as _superverse_v2_UpdateSessionModelResponse, UpdateSessionModelResponse__Output as _superverse_v2_UpdateSessionModelResponse__Output } from './superverse/v2/UpdateSessionModelResponse';
import type { UpdateTeamSpaceConnectionRequest as _superverse_v2_UpdateTeamSpaceConnectionRequest, UpdateTeamSpaceConnectionRequest__Output as _superverse_v2_UpdateTeamSpaceConnectionRequest__Output } from './superverse/v2/UpdateTeamSpaceConnectionRequest';
import type { UpdateTeamSpaceConnectionResponse as _superverse_v2_UpdateTeamSpaceConnectionResponse, UpdateTeamSpaceConnectionResponse__Output as _superverse_v2_UpdateTeamSpaceConnectionResponse__Output } from './superverse/v2/UpdateTeamSpaceConnectionResponse';
import type { UploadHeader as _superverse_v2_UploadHeader, UploadHeader__Output as _superverse_v2_UploadHeader__Output } from './superverse/v2/UploadHeader';
import type { ValidateProjectConfigurationRequest as _superverse_v2_ValidateProjectConfigurationRequest, ValidateProjectConfigurationRequest__Output as _superverse_v2_ValidateProjectConfigurationRequest__Output } from './superverse/v2/ValidateProjectConfigurationRequest';
import type { ValidateProjectConfigurationResponse as _superverse_v2_ValidateProjectConfigurationResponse, ValidateProjectConfigurationResponse__Output as _superverse_v2_ValidateProjectConfigurationResponse__Output } from './superverse/v2/ValidateProjectConfigurationResponse';
import type { WaitFlowHistoryRequest as _superverse_v2_WaitFlowHistoryRequest, WaitFlowHistoryRequest__Output as _superverse_v2_WaitFlowHistoryRequest__Output } from './superverse/v2/WaitFlowHistoryRequest';
import type { WaitFlowHistoryResponse as _superverse_v2_WaitFlowHistoryResponse, WaitFlowHistoryResponse__Output as _superverse_v2_WaitFlowHistoryResponse__Output } from './superverse/v2/WaitFlowHistoryResponse';
import type { WatchDeploymentRequest as _superverse_v2_WatchDeploymentRequest, WatchDeploymentRequest__Output as _superverse_v2_WatchDeploymentRequest__Output } from './superverse/v2/WatchDeploymentRequest';
import type { WatchDeploymentResponse as _superverse_v2_WatchDeploymentResponse, WatchDeploymentResponse__Output as _superverse_v2_WatchDeploymentResponse__Output } from './superverse/v2/WatchDeploymentResponse';
import type { WatchProjectRequest as _superverse_v2_WatchProjectRequest, WatchProjectRequest__Output as _superverse_v2_WatchProjectRequest__Output } from './superverse/v2/WatchProjectRequest';
import type { WatchProjectResponse as _superverse_v2_WatchProjectResponse, WatchProjectResponse__Output as _superverse_v2_WatchProjectResponse__Output } from './superverse/v2/WatchProjectResponse';
import type { WatchReleaseRequest as _superverse_v2_WatchReleaseRequest, WatchReleaseRequest__Output as _superverse_v2_WatchReleaseRequest__Output } from './superverse/v2/WatchReleaseRequest';
import type { WatchReleaseResponse as _superverse_v2_WatchReleaseResponse, WatchReleaseResponse__Output as _superverse_v2_WatchReleaseResponse__Output } from './superverse/v2/WatchReleaseResponse';
import type { WatchSessionRequest as _superverse_v2_WatchSessionRequest, WatchSessionRequest__Output as _superverse_v2_WatchSessionRequest__Output } from './superverse/v2/WatchSessionRequest';
import type { WatchSessionResponse as _superverse_v2_WatchSessionResponse, WatchSessionResponse__Output as _superverse_v2_WatchSessionResponse__Output } from './superverse/v2/WatchSessionResponse';

type SubtypeConstructor<Constructor extends new (...args: any) => any, Subtype> = {
  new(...args: ConstructorParameters<Constructor>): Subtype;
};

export interface ProtoGrpcType {
  google: {
    protobuf: {
      ListValue: MessageTypeDefinition<_google_protobuf_ListValue, _google_protobuf_ListValue__Output>
      NullValue: EnumTypeDefinition
      Struct: MessageTypeDefinition<_google_protobuf_Struct, _google_protobuf_Struct__Output>
      Timestamp: MessageTypeDefinition<_google_protobuf_Timestamp, _google_protobuf_Timestamp__Output>
      Value: MessageTypeDefinition<_google_protobuf_Value, _google_protobuf_Value__Output>
    }
  }
  superverse: {
    v2: {
      AccountLogin: MessageTypeDefinition<_superverse_v2_AccountLogin, _superverse_v2_AccountLogin__Output>
      AccountService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_AccountServiceClient> & { service: _superverse_v2_AccountServiceDefinition }
      AccountSnapshot: MessageTypeDefinition<_superverse_v2_AccountSnapshot, _superverse_v2_AccountSnapshot__Output>
      ActiveDeploymentReference: MessageTypeDefinition<_superverse_v2_ActiveDeploymentReference, _superverse_v2_ActiveDeploymentReference__Output>
      Actor: MessageTypeDefinition<_superverse_v2_Actor, _superverse_v2_Actor__Output>
      AddProjectMemberRequest: MessageTypeDefinition<_superverse_v2_AddProjectMemberRequest, _superverse_v2_AddProjectMemberRequest__Output>
      AddProjectMemberResponse: MessageTypeDefinition<_superverse_v2_AddProjectMemberResponse, _superverse_v2_AddProjectMemberResponse__Output>
      AddTeamSpaceMemberRequest: MessageTypeDefinition<_superverse_v2_AddTeamSpaceMemberRequest, _superverse_v2_AddTeamSpaceMemberRequest__Output>
      AddTeamSpaceMemberResponse: MessageTypeDefinition<_superverse_v2_AddTeamSpaceMemberResponse, _superverse_v2_AddTeamSpaceMemberResponse__Output>
      AgentAnswerQuestionsInput: MessageTypeDefinition<_superverse_v2_AgentAnswerQuestionsInput, _superverse_v2_AgentAnswerQuestionsInput__Output>
      AgentAttachment: MessageTypeDefinition<_superverse_v2_AgentAttachment, _superverse_v2_AgentAttachment__Output>
      AgentBackgroundCommand: MessageTypeDefinition<_superverse_v2_AgentBackgroundCommand, _superverse_v2_AgentBackgroundCommand__Output>
      AgentConfirmedRequirements: MessageTypeDefinition<_superverse_v2_AgentConfirmedRequirements, _superverse_v2_AgentConfirmedRequirements__Output>
      AgentDescription: MessageTypeDefinition<_superverse_v2_AgentDescription, _superverse_v2_AgentDescription__Output>
      AgentEvent: MessageTypeDefinition<_superverse_v2_AgentEvent, _superverse_v2_AgentEvent__Output>
      AgentFlowDesign: MessageTypeDefinition<_superverse_v2_AgentFlowDesign, _superverse_v2_AgentFlowDesign__Output>
      AgentFlowDesignAction: MessageTypeDefinition<_superverse_v2_AgentFlowDesignAction, _superverse_v2_AgentFlowDesignAction__Output>
      AgentFlowDesignAttribute: MessageTypeDefinition<_superverse_v2_AgentFlowDesignAttribute, _superverse_v2_AgentFlowDesignAttribute__Output>
      AgentFlowDesignChannel: MessageTypeDefinition<_superverse_v2_AgentFlowDesignChannel, _superverse_v2_AgentFlowDesignChannel__Output>
      AgentFlowDesignFlow: MessageTypeDefinition<_superverse_v2_AgentFlowDesignFlow, _superverse_v2_AgentFlowDesignFlow__Output>
      AgentFlowDesignReference: MessageTypeDefinition<_superverse_v2_AgentFlowDesignReference, _superverse_v2_AgentFlowDesignReference__Output>
      AgentFlowDesignSlot: EnumTypeDefinition
      AgentFlowDesignStep: MessageTypeDefinition<_superverse_v2_AgentFlowDesignStep, _superverse_v2_AgentFlowDesignStep__Output>
      AgentFlowDesignTransition: MessageTypeDefinition<_superverse_v2_AgentFlowDesignTransition, _superverse_v2_AgentFlowDesignTransition__Output>
      AgentFlowDesignWait: MessageTypeDefinition<_superverse_v2_AgentFlowDesignWait, _superverse_v2_AgentFlowDesignWait__Output>
      AgentHistoryPage: MessageTypeDefinition<_superverse_v2_AgentHistoryPage, _superverse_v2_AgentHistoryPage__Output>
      AgentInputConsumption: MessageTypeDefinition<_superverse_v2_AgentInputConsumption, _superverse_v2_AgentInputConsumption__Output>
      AgentMessage: MessageTypeDefinition<_superverse_v2_AgentMessage, _superverse_v2_AgentMessage__Output>
      AgentMockProgress: MessageTypeDefinition<_superverse_v2_AgentMockProgress, _superverse_v2_AgentMockProgress__Output>
      AgentMockScenario: MessageTypeDefinition<_superverse_v2_AgentMockScenario, _superverse_v2_AgentMockScenario__Output>
      AgentMockScenarioArgument: MessageTypeDefinition<_superverse_v2_AgentMockScenarioArgument, _superverse_v2_AgentMockScenarioArgument__Output>
      AgentMockScenarioInput: MessageTypeDefinition<_superverse_v2_AgentMockScenarioInput, _superverse_v2_AgentMockScenarioInput__Output>
      AgentModel: EnumTypeDefinition
      AgentModelCallMetrics: MessageTypeDefinition<_superverse_v2_AgentModelCallMetrics, _superverse_v2_AgentModelCallMetrics__Output>
      AgentModelChoice: MessageTypeDefinition<_superverse_v2_AgentModelChoice, _superverse_v2_AgentModelChoice__Output>
      AgentNotification: MessageTypeDefinition<_superverse_v2_AgentNotification, _superverse_v2_AgentNotification__Output>
      AgentPendingApproval: MessageTypeDefinition<_superverse_v2_AgentPendingApproval, _superverse_v2_AgentPendingApproval__Output>
      AgentPendingTimer: MessageTypeDefinition<_superverse_v2_AgentPendingTimer, _superverse_v2_AgentPendingTimer__Output>
      AgentPendingUserInput: MessageTypeDefinition<_superverse_v2_AgentPendingUserInput, _superverse_v2_AgentPendingUserInput__Output>
      AgentPendingUserMessage: MessageTypeDefinition<_superverse_v2_AgentPendingUserMessage, _superverse_v2_AgentPendingUserMessage__Output>
      AgentRequirementConnection: MessageTypeDefinition<_superverse_v2_AgentRequirementConnection, _superverse_v2_AgentRequirementConnection__Output>
      AgentRequirementFeature: MessageTypeDefinition<_superverse_v2_AgentRequirementFeature, _superverse_v2_AgentRequirementFeature__Output>
      AgentRequirements: MessageTypeDefinition<_superverse_v2_AgentRequirements, _superverse_v2_AgentRequirements__Output>
      AgentRequirementsDraft: MessageTypeDefinition<_superverse_v2_AgentRequirementsDraft, _superverse_v2_AgentRequirementsDraft__Output>
      AgentRequirementsDraftConnection: MessageTypeDefinition<_superverse_v2_AgentRequirementsDraftConnection, _superverse_v2_AgentRequirementsDraftConnection__Output>
      AgentRequirementsDraftFlow: MessageTypeDefinition<_superverse_v2_AgentRequirementsDraftFlow, _superverse_v2_AgentRequirementsDraftFlow__Output>
      AgentSequencedMessage: MessageTypeDefinition<_superverse_v2_AgentSequencedMessage, _superverse_v2_AgentSequencedMessage__Output>
      AgentService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_AgentServiceClient> & { service: _superverse_v2_AgentServiceDefinition }
      AgentSnapshot: MessageTypeDefinition<_superverse_v2_AgentSnapshot, _superverse_v2_AgentSnapshot__Output>
      AgentStartingExample: MessageTypeDefinition<_superverse_v2_AgentStartingExample, _superverse_v2_AgentStartingExample__Output>
      AgentStartingExampleBuild: MessageTypeDefinition<_superverse_v2_AgentStartingExampleBuild, _superverse_v2_AgentStartingExampleBuild__Output>
      AgentSubagent: MessageTypeDefinition<_superverse_v2_AgentSubagent, _superverse_v2_AgentSubagent__Output>
      AgentTodoItem: MessageTypeDefinition<_superverse_v2_AgentTodoItem, _superverse_v2_AgentTodoItem__Output>
      AgentTodoList: MessageTypeDefinition<_superverse_v2_AgentTodoList, _superverse_v2_AgentTodoList__Output>
      AgentToolCall: MessageTypeDefinition<_superverse_v2_AgentToolCall, _superverse_v2_AgentToolCall__Output>
      AgentToolExecutionMetrics: MessageTypeDefinition<_superverse_v2_AgentToolExecutionMetrics, _superverse_v2_AgentToolExecutionMetrics__Output>
      AgentUserInputAnswer: MessageTypeDefinition<_superverse_v2_AgentUserInputAnswer, _superverse_v2_AgentUserInputAnswer__Output>
      AgentUserInputOption: MessageTypeDefinition<_superverse_v2_AgentUserInputOption, _superverse_v2_AgentUserInputOption__Output>
      AgentUserInputQuestion: MessageTypeDefinition<_superverse_v2_AgentUserInputQuestion, _superverse_v2_AgentUserInputQuestion__Output>
      AgentUserMessage: MessageTypeDefinition<_superverse_v2_AgentUserMessage, _superverse_v2_AgentUserMessage__Output>
      AnswerQuestionsRequest: MessageTypeDefinition<_superverse_v2_AnswerQuestionsRequest, _superverse_v2_AnswerQuestionsRequest__Output>
      AnswerQuestionsResponse: MessageTypeDefinition<_superverse_v2_AnswerQuestionsResponse, _superverse_v2_AnswerQuestionsResponse__Output>
      AppManifestSnapshot: MessageTypeDefinition<_superverse_v2_AppManifestSnapshot, _superverse_v2_AppManifestSnapshot__Output>
      ApplicationEnvironmentService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_ApplicationEnvironmentServiceClient> & { service: _superverse_v2_ApplicationEnvironmentServiceDefinition }
      AppliedPreviewConfiguration: MessageTypeDefinition<_superverse_v2_AppliedPreviewConfiguration, _superverse_v2_AppliedPreviewConfiguration__Output>
      Attachment: MessageTypeDefinition<_superverse_v2_Attachment, _superverse_v2_Attachment__Output>
      AttachmentDisposition: EnumTypeDefinition
      AttachmentDownload: MessageTypeDefinition<_superverse_v2_AttachmentDownload, _superverse_v2_AttachmentDownload__Output>
      AttachmentFailure: MessageTypeDefinition<_superverse_v2_AttachmentFailure, _superverse_v2_AttachmentFailure__Output>
      AttachmentKind: EnumTypeDefinition
      AttachmentService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_AttachmentServiceClient> & { service: _superverse_v2_AttachmentServiceDefinition }
      AttachmentStatus: EnumTypeDefinition
      AttachmentSummary: MessageTypeDefinition<_superverse_v2_AttachmentSummary, _superverse_v2_AttachmentSummary__Output>
      AttachmentUploadTarget: MessageTypeDefinition<_superverse_v2_AttachmentUploadTarget, _superverse_v2_AttachmentUploadTarget__Output>
      AutoDeployStatus: EnumTypeDefinition
      CallerContext: MessageTypeDefinition<_superverse_v2_CallerContext, _superverse_v2_CallerContext__Output>
      CancelDeploymentRequest: MessageTypeDefinition<_superverse_v2_CancelDeploymentRequest, _superverse_v2_CancelDeploymentRequest__Output>
      CancelDeploymentResponse: MessageTypeDefinition<_superverse_v2_CancelDeploymentResponse, _superverse_v2_CancelDeploymentResponse__Output>
      CancelReleaseBuildRequest: MessageTypeDefinition<_superverse_v2_CancelReleaseBuildRequest, _superverse_v2_CancelReleaseBuildRequest__Output>
      CancelReleaseBuildResponse: MessageTypeDefinition<_superverse_v2_CancelReleaseBuildResponse, _superverse_v2_CancelReleaseBuildResponse__Output>
      Cancellation: MessageTypeDefinition<_superverse_v2_Cancellation, _superverse_v2_Cancellation__Output>
      CommitVersion: MessageTypeDefinition<_superverse_v2_CommitVersion, _superverse_v2_CommitVersion__Output>
      CompleteAttachmentUploadRequest: MessageTypeDefinition<_superverse_v2_CompleteAttachmentUploadRequest, _superverse_v2_CompleteAttachmentUploadRequest__Output>
      CompleteAttachmentUploadResponse: MessageTypeDefinition<_superverse_v2_CompleteAttachmentUploadResponse, _superverse_v2_CompleteAttachmentUploadResponse__Output>
      CompleteTeamSpaceConnectionOAuthRequest: MessageTypeDefinition<_superverse_v2_CompleteTeamSpaceConnectionOAuthRequest, _superverse_v2_CompleteTeamSpaceConnectionOAuthRequest__Output>
      CompleteTeamSpaceConnectionOAuthResponse: MessageTypeDefinition<_superverse_v2_CompleteTeamSpaceConnectionOAuthResponse, _superverse_v2_CompleteTeamSpaceConnectionOAuthResponse__Output>
      ConfigurationFollowRefusal: EnumTypeDefinition
      ConfigurationItem: MessageTypeDefinition<_superverse_v2_ConfigurationItem, _superverse_v2_ConfigurationItem__Output>
      ConfigurationItemKey: MessageTypeDefinition<_superverse_v2_ConfigurationItemKey, _superverse_v2_ConfigurationItemKey__Output>
      ConfigurationItemWarning: EnumTypeDefinition
      ConfigurationRequirementGap: MessageTypeDefinition<_superverse_v2_ConfigurationRequirementGap, _superverse_v2_ConfigurationRequirementGap__Output>
      ConfigurationStatus: EnumTypeDefinition
      ConnectionClientSource: EnumTypeDefinition
      ConnectorCatalogEntry: MessageTypeDefinition<_superverse_v2_ConnectorCatalogEntry, _superverse_v2_ConnectorCatalogEntry__Output>
      ConnectorContribution: MessageTypeDefinition<_superverse_v2_ConnectorContribution, _superverse_v2_ConnectorContribution__Output>
      ConnectorRequirement: MessageTypeDefinition<_superverse_v2_ConnectorRequirement, _superverse_v2_ConnectorRequirement__Output>
      ConnectorService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_ConnectorServiceClient> & { service: _superverse_v2_ConnectorServiceDefinition }
      ConnectorTriggerBinding: MessageTypeDefinition<_superverse_v2_ConnectorTriggerBinding, _superverse_v2_ConnectorTriggerBinding__Output>
      CreateAttachmentUploadRequest: MessageTypeDefinition<_superverse_v2_CreateAttachmentUploadRequest, _superverse_v2_CreateAttachmentUploadRequest__Output>
      CreateAttachmentUploadResponse: MessageTypeDefinition<_superverse_v2_CreateAttachmentUploadResponse, _superverse_v2_CreateAttachmentUploadResponse__Output>
      CreateProjectRequest: MessageTypeDefinition<_superverse_v2_CreateProjectRequest, _superverse_v2_CreateProjectRequest__Output>
      CreateProjectResponse: MessageTypeDefinition<_superverse_v2_CreateProjectResponse, _superverse_v2_CreateProjectResponse__Output>
      CreateRemoteAgentPairingRequest: MessageTypeDefinition<_superverse_v2_CreateRemoteAgentPairingRequest, _superverse_v2_CreateRemoteAgentPairingRequest__Output>
      CreateRemoteAgentPairingResponse: MessageTypeDefinition<_superverse_v2_CreateRemoteAgentPairingResponse, _superverse_v2_CreateRemoteAgentPairingResponse__Output>
      CreateTeamSpaceConnectionRequest: MessageTypeDefinition<_superverse_v2_CreateTeamSpaceConnectionRequest, _superverse_v2_CreateTeamSpaceConnectionRequest__Output>
      CreateTeamSpaceConnectionResponse: MessageTypeDefinition<_superverse_v2_CreateTeamSpaceConnectionResponse, _superverse_v2_CreateTeamSpaceConnectionResponse__Output>
      CreateTeamSpaceRequest: MessageTypeDefinition<_superverse_v2_CreateTeamSpaceRequest, _superverse_v2_CreateTeamSpaceRequest__Output>
      CreateTeamSpaceResponse: MessageTypeDefinition<_superverse_v2_CreateTeamSpaceResponse, _superverse_v2_CreateTeamSpaceResponse__Output>
      CredentialStatus: EnumTypeDefinition
      DecideEvolveProposalRequest: MessageTypeDefinition<_superverse_v2_DecideEvolveProposalRequest, _superverse_v2_DecideEvolveProposalRequest__Output>
      DecideEvolveProposalResponse: MessageTypeDefinition<_superverse_v2_DecideEvolveProposalResponse, _superverse_v2_DecideEvolveProposalResponse__Output>
      DeleteAttachmentRequest: MessageTypeDefinition<_superverse_v2_DeleteAttachmentRequest, _superverse_v2_DeleteAttachmentRequest__Output>
      DeleteAttachmentResponse: MessageTypeDefinition<_superverse_v2_DeleteAttachmentResponse, _superverse_v2_DeleteAttachmentResponse__Output>
      DeleteFlowChannelMessageRequest: MessageTypeDefinition<_superverse_v2_DeleteFlowChannelMessageRequest, _superverse_v2_DeleteFlowChannelMessageRequest__Output>
      DeleteFlowChannelMessageResponse: MessageTypeDefinition<_superverse_v2_DeleteFlowChannelMessageResponse, _superverse_v2_DeleteFlowChannelMessageResponse__Output>
      DeleteModelCredentialRequest: MessageTypeDefinition<_superverse_v2_DeleteModelCredentialRequest, _superverse_v2_DeleteModelCredentialRequest__Output>
      DeleteModelCredentialResponse: MessageTypeDefinition<_superverse_v2_DeleteModelCredentialResponse, _superverse_v2_DeleteModelCredentialResponse__Output>
      DeleteProjectRequest: MessageTypeDefinition<_superverse_v2_DeleteProjectRequest, _superverse_v2_DeleteProjectRequest__Output>
      DeleteProjectResponse: MessageTypeDefinition<_superverse_v2_DeleteProjectResponse, _superverse_v2_DeleteProjectResponse__Output>
      DeleteQueuedMessageRequest: MessageTypeDefinition<_superverse_v2_DeleteQueuedMessageRequest, _superverse_v2_DeleteQueuedMessageRequest__Output>
      DeleteQueuedMessageResponse: MessageTypeDefinition<_superverse_v2_DeleteQueuedMessageResponse, _superverse_v2_DeleteQueuedMessageResponse__Output>
      DeleteTeamSpaceConnectionRequest: MessageTypeDefinition<_superverse_v2_DeleteTeamSpaceConnectionRequest, _superverse_v2_DeleteTeamSpaceConnectionRequest__Output>
      DeleteTeamSpaceConnectionResponse: MessageTypeDefinition<_superverse_v2_DeleteTeamSpaceConnectionResponse, _superverse_v2_DeleteTeamSpaceConnectionResponse__Output>
      DeleteTeamSpaceRequest: MessageTypeDefinition<_superverse_v2_DeleteTeamSpaceRequest, _superverse_v2_DeleteTeamSpaceRequest__Output>
      DeleteTeamSpaceResponse: MessageTypeDefinition<_superverse_v2_DeleteTeamSpaceResponse, _superverse_v2_DeleteTeamSpaceResponse__Output>
      DeploymentService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_DeploymentServiceClient> & { service: _superverse_v2_DeploymentServiceDefinition }
      DeploymentSnapshot: MessageTypeDefinition<_superverse_v2_DeploymentSnapshot, _superverse_v2_DeploymentSnapshot__Output>
      DeploymentStatus: EnumTypeDefinition
      DeriveLiveConfigurationRequest: MessageTypeDefinition<_superverse_v2_DeriveLiveConfigurationRequest, _superverse_v2_DeriveLiveConfigurationRequest__Output>
      DeriveLiveConfigurationResponse: MessageTypeDefinition<_superverse_v2_DeriveLiveConfigurationResponse, _superverse_v2_DeriveLiveConfigurationResponse__Output>
      EnvironmentEvent: MessageTypeDefinition<_superverse_v2_EnvironmentEvent, _superverse_v2_EnvironmentEvent__Output>
      Evolve: MessageTypeDefinition<_superverse_v2_Evolve, _superverse_v2_Evolve__Output>
      EvolveExport: MessageTypeDefinition<_superverse_v2_EvolveExport, _superverse_v2_EvolveExport__Output>
      EvolveGoal: MessageTypeDefinition<_superverse_v2_EvolveGoal, _superverse_v2_EvolveGoal__Output>
      EvolveGoalDirection: EnumTypeDefinition
      EvolveGoalKind: EnumTypeDefinition
      EvolveGoalValue: MessageTypeDefinition<_superverse_v2_EvolveGoalValue, _superverse_v2_EvolveGoalValue__Output>
      EvolveGoals: MessageTypeDefinition<_superverse_v2_EvolveGoals, _superverse_v2_EvolveGoals__Output>
      EvolveGoalsStatus: EnumTypeDefinition
      EvolveLive: MessageTypeDefinition<_superverse_v2_EvolveLive, _superverse_v2_EvolveLive__Output>
      EvolveProposal: MessageTypeDefinition<_superverse_v2_EvolveProposal, _superverse_v2_EvolveProposal__Output>
      EvolveProposalDecision: EnumTypeDefinition
      EvolveProposalDecisionRecord: MessageTypeDefinition<_superverse_v2_EvolveProposalDecisionRecord, _superverse_v2_EvolveProposalDecisionRecord__Output>
      EvolveProposalKind: EnumTypeDefinition
      EvolveProposalRefusal: EnumTypeDefinition
      EvolveProposalStatus: EnumTypeDefinition
      EvolveReview: MessageTypeDefinition<_superverse_v2_EvolveReview, _superverse_v2_EvolveReview__Output>
      EvolveReviewPeriod: EnumTypeDefinition
      EvolveReviewReason: EnumTypeDefinition
      EvolveReviewRefusal: EnumTypeDefinition
      EvolveReviewStatus: EnumTypeDefinition
      EvolveReviewTrigger: EnumTypeDefinition
      EvolveSchedule: MessageTypeDefinition<_superverse_v2_EvolveSchedule, _superverse_v2_EvolveSchedule__Output>
      EvolveService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_EvolveServiceClient> & { service: _superverse_v2_EvolveServiceDefinition }
      ExampleReference: MessageTypeDefinition<_superverse_v2_ExampleReference, _superverse_v2_ExampleReference__Output>
      Failure: MessageTypeDefinition<_superverse_v2_Failure, _superverse_v2_Failure__Output>
      FailureOwner: EnumTypeDefinition
      FailureRemedy: EnumTypeDefinition
      FlowApiContext: MessageTypeDefinition<_superverse_v2_FlowApiContext, _superverse_v2_FlowApiContext__Output>
      FlowApiDocument: MessageTypeDefinition<_superverse_v2_FlowApiDocument, _superverse_v2_FlowApiDocument__Output>
      FlowApiTarget: MessageTypeDefinition<_superverse_v2_FlowApiTarget, _superverse_v2_FlowApiTarget__Output>
      FlowBlobReference: MessageTypeDefinition<_superverse_v2_FlowBlobReference, _superverse_v2_FlowBlobReference__Output>
      FlowDefinition: MessageTypeDefinition<_superverse_v2_FlowDefinition, _superverse_v2_FlowDefinition__Output>
      FlowDiagnosticsService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_FlowDiagnosticsServiceClient> & { service: _superverse_v2_FlowDiagnosticsServiceDefinition }
      FlowRunService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_FlowRunServiceClient> & { service: _superverse_v2_FlowRunServiceDefinition }
      FlowSearchFilter: MessageTypeDefinition<_superverse_v2_FlowSearchFilter, _superverse_v2_FlowSearchFilter__Output>
      GetApplicationEnvironmentRequest: MessageTypeDefinition<_superverse_v2_GetApplicationEnvironmentRequest, _superverse_v2_GetApplicationEnvironmentRequest__Output>
      GetApplicationEnvironmentResponse: MessageTypeDefinition<_superverse_v2_GetApplicationEnvironmentResponse, _superverse_v2_GetApplicationEnvironmentResponse__Output>
      GetAttachmentDownloadsRequest: MessageTypeDefinition<_superverse_v2_GetAttachmentDownloadsRequest, _superverse_v2_GetAttachmentDownloadsRequest__Output>
      GetAttachmentDownloadsResponse: MessageTypeDefinition<_superverse_v2_GetAttachmentDownloadsResponse, _superverse_v2_GetAttachmentDownloadsResponse__Output>
      GetCurrentAccountRequest: MessageTypeDefinition<_superverse_v2_GetCurrentAccountRequest, _superverse_v2_GetCurrentAccountRequest__Output>
      GetCurrentAccountResponse: MessageTypeDefinition<_superverse_v2_GetCurrentAccountResponse, _superverse_v2_GetCurrentAccountResponse__Output>
      GetDeploymentRequest: MessageTypeDefinition<_superverse_v2_GetDeploymentRequest, _superverse_v2_GetDeploymentRequest__Output>
      GetDeploymentResponse: MessageTypeDefinition<_superverse_v2_GetDeploymentResponse, _superverse_v2_GetDeploymentResponse__Output>
      GetEvolveProposalRequest: MessageTypeDefinition<_superverse_v2_GetEvolveProposalRequest, _superverse_v2_GetEvolveProposalRequest__Output>
      GetEvolveProposalResponse: MessageTypeDefinition<_superverse_v2_GetEvolveProposalResponse, _superverse_v2_GetEvolveProposalResponse__Output>
      GetEvolveRequest: MessageTypeDefinition<_superverse_v2_GetEvolveRequest, _superverse_v2_GetEvolveRequest__Output>
      GetEvolveResponse: MessageTypeDefinition<_superverse_v2_GetEvolveResponse, _superverse_v2_GetEvolveResponse__Output>
      GetFlowCatalogRequest: MessageTypeDefinition<_superverse_v2_GetFlowCatalogRequest, _superverse_v2_GetFlowCatalogRequest__Output>
      GetFlowCatalogResponse: MessageTypeDefinition<_superverse_v2_GetFlowCatalogResponse, _superverse_v2_GetFlowCatalogResponse__Output>
      GetFlowDefinitionsRequest: MessageTypeDefinition<_superverse_v2_GetFlowDefinitionsRequest, _superverse_v2_GetFlowDefinitionsRequest__Output>
      GetFlowDefinitionsResponse: MessageTypeDefinition<_superverse_v2_GetFlowDefinitionsResponse, _superverse_v2_GetFlowDefinitionsResponse__Output>
      GetFlowDesignRequest: MessageTypeDefinition<_superverse_v2_GetFlowDesignRequest, _superverse_v2_GetFlowDesignRequest__Output>
      GetFlowDesignResponse: MessageTypeDefinition<_superverse_v2_GetFlowDesignResponse, _superverse_v2_GetFlowDesignResponse__Output>
      GetFlowDisplayRequest: MessageTypeDefinition<_superverse_v2_GetFlowDisplayRequest, _superverse_v2_GetFlowDisplayRequest__Output>
      GetFlowDisplayResponse: MessageTypeDefinition<_superverse_v2_GetFlowDisplayResponse, _superverse_v2_GetFlowDisplayResponse__Output>
      GetFlowHistoryRequest: MessageTypeDefinition<_superverse_v2_GetFlowHistoryRequest, _superverse_v2_GetFlowHistoryRequest__Output>
      GetFlowHistoryResponse: MessageTypeDefinition<_superverse_v2_GetFlowHistoryResponse, _superverse_v2_GetFlowHistoryResponse__Output>
      GetFlowStateRequest: MessageTypeDefinition<_superverse_v2_GetFlowStateRequest, _superverse_v2_GetFlowStateRequest__Output>
      GetFlowStateResponse: MessageTypeDefinition<_superverse_v2_GetFlowStateResponse, _superverse_v2_GetFlowStateResponse__Output>
      GetFlowSummaryRequest: MessageTypeDefinition<_superverse_v2_GetFlowSummaryRequest, _superverse_v2_GetFlowSummaryRequest__Output>
      GetFlowSummaryResponse: MessageTypeDefinition<_superverse_v2_GetFlowSummaryResponse, _superverse_v2_GetFlowSummaryResponse__Output>
      GetModelCredentialInfoRequest: MessageTypeDefinition<_superverse_v2_GetModelCredentialInfoRequest, _superverse_v2_GetModelCredentialInfoRequest__Output>
      GetModelCredentialInfoResponse: MessageTypeDefinition<_superverse_v2_GetModelCredentialInfoResponse, _superverse_v2_GetModelCredentialInfoResponse__Output>
      GetPreviewDefinitionsRequest: MessageTypeDefinition<_superverse_v2_GetPreviewDefinitionsRequest, _superverse_v2_GetPreviewDefinitionsRequest__Output>
      GetPreviewDefinitionsResponse: MessageTypeDefinition<_superverse_v2_GetPreviewDefinitionsResponse, _superverse_v2_GetPreviewDefinitionsResponse__Output>
      GetPreviewRequest: MessageTypeDefinition<_superverse_v2_GetPreviewRequest, _superverse_v2_GetPreviewRequest__Output>
      GetPreviewResponse: MessageTypeDefinition<_superverse_v2_GetPreviewResponse, _superverse_v2_GetPreviewResponse__Output>
      GetProjectConfigurationRequest: MessageTypeDefinition<_superverse_v2_GetProjectConfigurationRequest, _superverse_v2_GetProjectConfigurationRequest__Output>
      GetProjectConfigurationResponse: MessageTypeDefinition<_superverse_v2_GetProjectConfigurationResponse, _superverse_v2_GetProjectConfigurationResponse__Output>
      GetProjectRequest: MessageTypeDefinition<_superverse_v2_GetProjectRequest, _superverse_v2_GetProjectRequest__Output>
      GetProjectResponse: MessageTypeDefinition<_superverse_v2_GetProjectResponse, _superverse_v2_GetProjectResponse__Output>
      GetReleaseBuildLogRequest: MessageTypeDefinition<_superverse_v2_GetReleaseBuildLogRequest, _superverse_v2_GetReleaseBuildLogRequest__Output>
      GetReleaseBuildLogResponse: MessageTypeDefinition<_superverse_v2_GetReleaseBuildLogResponse, _superverse_v2_GetReleaseBuildLogResponse__Output>
      GetReleaseRequest: MessageTypeDefinition<_superverse_v2_GetReleaseRequest, _superverse_v2_GetReleaseRequest__Output>
      GetReleaseResponse: MessageTypeDefinition<_superverse_v2_GetReleaseResponse, _superverse_v2_GetReleaseResponse__Output>
      GetSessionRequest: MessageTypeDefinition<_superverse_v2_GetSessionRequest, _superverse_v2_GetSessionRequest__Output>
      GetSessionResponse: MessageTypeDefinition<_superverse_v2_GetSessionResponse, _superverse_v2_GetSessionResponse__Output>
      GetTeamSpaceConnectionRequest: MessageTypeDefinition<_superverse_v2_GetTeamSpaceConnectionRequest, _superverse_v2_GetTeamSpaceConnectionRequest__Output>
      GetTeamSpaceConnectionResponse: MessageTypeDefinition<_superverse_v2_GetTeamSpaceConnectionResponse, _superverse_v2_GetTeamSpaceConnectionResponse__Output>
      GetTeamSpaceOverviewRequest: MessageTypeDefinition<_superverse_v2_GetTeamSpaceOverviewRequest, _superverse_v2_GetTeamSpaceOverviewRequest__Output>
      GetTeamSpaceOverviewResponse: MessageTypeDefinition<_superverse_v2_GetTeamSpaceOverviewResponse, _superverse_v2_GetTeamSpaceOverviewResponse__Output>
      GetTeamSpaceRequest: MessageTypeDefinition<_superverse_v2_GetTeamSpaceRequest, _superverse_v2_GetTeamSpaceRequest__Output>
      GetTeamSpaceResponse: MessageTypeDefinition<_superverse_v2_GetTeamSpaceResponse, _superverse_v2_GetTeamSpaceResponse__Output>
      InterruptAgentRequest: MessageTypeDefinition<_superverse_v2_InterruptAgentRequest, _superverse_v2_InterruptAgentRequest__Output>
      InterruptAgentResponse: MessageTypeDefinition<_superverse_v2_InterruptAgentResponse, _superverse_v2_InterruptAgentResponse__Output>
      InvokeFlowActionRequest: MessageTypeDefinition<_superverse_v2_InvokeFlowActionRequest, _superverse_v2_InvokeFlowActionRequest__Output>
      InvokeFlowActionResponse: MessageTypeDefinition<_superverse_v2_InvokeFlowActionResponse, _superverse_v2_InvokeFlowActionResponse__Output>
      InvokeProviderCommandRequest: MessageTypeDefinition<_superverse_v2_InvokeProviderCommandRequest, _superverse_v2_InvokeProviderCommandRequest__Output>
      InvokeProviderCommandResponse: MessageTypeDefinition<_superverse_v2_InvokeProviderCommandResponse, _superverse_v2_InvokeProviderCommandResponse__Output>
      InvokeTeamSpaceConnectionCommandRequest: MessageTypeDefinition<_superverse_v2_InvokeTeamSpaceConnectionCommandRequest, _superverse_v2_InvokeTeamSpaceConnectionCommandRequest__Output>
      InvokeTeamSpaceConnectionCommandResponse: MessageTypeDefinition<_superverse_v2_InvokeTeamSpaceConnectionCommandResponse, _superverse_v2_InvokeTeamSpaceConnectionCommandResponse__Output>
      ListAgentModelsRequest: MessageTypeDefinition<_superverse_v2_ListAgentModelsRequest, _superverse_v2_ListAgentModelsRequest__Output>
      ListAgentModelsResponse: MessageTypeDefinition<_superverse_v2_ListAgentModelsResponse, _superverse_v2_ListAgentModelsResponse__Output>
      ListArchivedMessagesRequest: MessageTypeDefinition<_superverse_v2_ListArchivedMessagesRequest, _superverse_v2_ListArchivedMessagesRequest__Output>
      ListArchivedMessagesResponse: MessageTypeDefinition<_superverse_v2_ListArchivedMessagesResponse, _superverse_v2_ListArchivedMessagesResponse__Output>
      ListConnectionsRequest: MessageTypeDefinition<_superverse_v2_ListConnectionsRequest, _superverse_v2_ListConnectionsRequest__Output>
      ListConnectionsResponse: MessageTypeDefinition<_superverse_v2_ListConnectionsResponse, _superverse_v2_ListConnectionsResponse__Output>
      ListConnectorCatalogRequest: MessageTypeDefinition<_superverse_v2_ListConnectorCatalogRequest, _superverse_v2_ListConnectorCatalogRequest__Output>
      ListConnectorCatalogResponse: MessageTypeDefinition<_superverse_v2_ListConnectorCatalogResponse, _superverse_v2_ListConnectorCatalogResponse__Output>
      ListDeploymentsRequest: MessageTypeDefinition<_superverse_v2_ListDeploymentsRequest, _superverse_v2_ListDeploymentsRequest__Output>
      ListDeploymentsResponse: MessageTypeDefinition<_superverse_v2_ListDeploymentsResponse, _superverse_v2_ListDeploymentsResponse__Output>
      ListEnvironmentEventsRequest: MessageTypeDefinition<_superverse_v2_ListEnvironmentEventsRequest, _superverse_v2_ListEnvironmentEventsRequest__Output>
      ListEnvironmentEventsResponse: MessageTypeDefinition<_superverse_v2_ListEnvironmentEventsResponse, _superverse_v2_ListEnvironmentEventsResponse__Output>
      ListMockScenariosRequest: MessageTypeDefinition<_superverse_v2_ListMockScenariosRequest, _superverse_v2_ListMockScenariosRequest__Output>
      ListMockScenariosResponse: MessageTypeDefinition<_superverse_v2_ListMockScenariosResponse, _superverse_v2_ListMockScenariosResponse__Output>
      ListModelCredentialModelsRequest: MessageTypeDefinition<_superverse_v2_ListModelCredentialModelsRequest, _superverse_v2_ListModelCredentialModelsRequest__Output>
      ListModelCredentialModelsResponse: MessageTypeDefinition<_superverse_v2_ListModelCredentialModelsResponse, _superverse_v2_ListModelCredentialModelsResponse__Output>
      ListProjectMembersRequest: MessageTypeDefinition<_superverse_v2_ListProjectMembersRequest, _superverse_v2_ListProjectMembersRequest__Output>
      ListProjectMembersResponse: MessageTypeDefinition<_superverse_v2_ListProjectMembersResponse, _superverse_v2_ListProjectMembersResponse__Output>
      ListProjectsRequest: MessageTypeDefinition<_superverse_v2_ListProjectsRequest, _superverse_v2_ListProjectsRequest__Output>
      ListProjectsResponse: MessageTypeDefinition<_superverse_v2_ListProjectsResponse, _superverse_v2_ListProjectsResponse__Output>
      ListRecentTriggerEventsRequest: MessageTypeDefinition<_superverse_v2_ListRecentTriggerEventsRequest, _superverse_v2_ListRecentTriggerEventsRequest__Output>
      ListRecentTriggerEventsResponse: MessageTypeDefinition<_superverse_v2_ListRecentTriggerEventsResponse, _superverse_v2_ListRecentTriggerEventsResponse__Output>
      ListReleaseCandidatesRequest: MessageTypeDefinition<_superverse_v2_ListReleaseCandidatesRequest, _superverse_v2_ListReleaseCandidatesRequest__Output>
      ListReleaseCandidatesResponse: MessageTypeDefinition<_superverse_v2_ListReleaseCandidatesResponse, _superverse_v2_ListReleaseCandidatesResponse__Output>
      ListReleasesRequest: MessageTypeDefinition<_superverse_v2_ListReleasesRequest, _superverse_v2_ListReleasesRequest__Output>
      ListReleasesResponse: MessageTypeDefinition<_superverse_v2_ListReleasesResponse, _superverse_v2_ListReleasesResponse__Output>
      ListRemoteAgentConnectionsRequest: MessageTypeDefinition<_superverse_v2_ListRemoteAgentConnectionsRequest, _superverse_v2_ListRemoteAgentConnectionsRequest__Output>
      ListRemoteAgentConnectionsResponse: MessageTypeDefinition<_superverse_v2_ListRemoteAgentConnectionsResponse, _superverse_v2_ListRemoteAgentConnectionsResponse__Output>
      ListTeamSpaceConnectionsRequest: MessageTypeDefinition<_superverse_v2_ListTeamSpaceConnectionsRequest, _superverse_v2_ListTeamSpaceConnectionsRequest__Output>
      ListTeamSpaceConnectionsResponse: MessageTypeDefinition<_superverse_v2_ListTeamSpaceConnectionsResponse, _superverse_v2_ListTeamSpaceConnectionsResponse__Output>
      ListTeamSpaceMembersRequest: MessageTypeDefinition<_superverse_v2_ListTeamSpaceMembersRequest, _superverse_v2_ListTeamSpaceMembersRequest__Output>
      ListTeamSpaceMembersResponse: MessageTypeDefinition<_superverse_v2_ListTeamSpaceMembersResponse, _superverse_v2_ListTeamSpaceMembersResponse__Output>
      ListTeamSpacesRequest: MessageTypeDefinition<_superverse_v2_ListTeamSpacesRequest, _superverse_v2_ListTeamSpacesRequest__Output>
      ListTeamSpacesResponse: MessageTypeDefinition<_superverse_v2_ListTeamSpacesResponse, _superverse_v2_ListTeamSpacesResponse__Output>
      LoadFlowBlobsRequest: MessageTypeDefinition<_superverse_v2_LoadFlowBlobsRequest, _superverse_v2_LoadFlowBlobsRequest__Output>
      LoadFlowBlobsResponse: MessageTypeDefinition<_superverse_v2_LoadFlowBlobsResponse, _superverse_v2_LoadFlowBlobsResponse__Output>
      LoginRequest: MessageTypeDefinition<_superverse_v2_LoginRequest, _superverse_v2_LoginRequest__Output>
      LoginResponse: MessageTypeDefinition<_superverse_v2_LoginResponse, _superverse_v2_LoginResponse__Output>
      LogoutRequest: MessageTypeDefinition<_superverse_v2_LogoutRequest, _superverse_v2_LogoutRequest__Output>
      LogoutResponse: MessageTypeDefinition<_superverse_v2_LogoutResponse, _superverse_v2_LogoutResponse__Output>
      ManagedRepositorySource: MessageTypeDefinition<_superverse_v2_ManagedRepositorySource, _superverse_v2_ManagedRepositorySource__Output>
      ModelCredentialInfo: MessageTypeDefinition<_superverse_v2_ModelCredentialInfo, _superverse_v2_ModelCredentialInfo__Output>
      ModelCredentialService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_ModelCredentialServiceClient> & { service: _superverse_v2_ModelCredentialServiceDefinition }
      ModelCredentialStatus: EnumTypeDefinition
      ModelDiscoveryResult: EnumTypeDefinition
      ModelProvider: EnumTypeDefinition
      NewTeamSpaceConnection: MessageTypeDefinition<_superverse_v2_NewTeamSpaceConnection, _superverse_v2_NewTeamSpaceConnection__Output>
      ObjectReference: MessageTypeDefinition<_superverse_v2_ObjectReference, _superverse_v2_ObjectReference__Output>
      PhaseTiming: MessageTypeDefinition<_superverse_v2_PhaseTiming, _superverse_v2_PhaseTiming__Output>
      PreparePreviewSourceRequest: MessageTypeDefinition<_superverse_v2_PreparePreviewSourceRequest, _superverse_v2_PreparePreviewSourceRequest__Output>
      PreparePreviewSourceResponse: MessageTypeDefinition<_superverse_v2_PreparePreviewSourceResponse, _superverse_v2_PreparePreviewSourceResponse__Output>
      PrepareReleaseRequest: MessageTypeDefinition<_superverse_v2_PrepareReleaseRequest, _superverse_v2_PrepareReleaseRequest__Output>
      PrepareReleaseResponse: MessageTypeDefinition<_superverse_v2_PrepareReleaseResponse, _superverse_v2_PrepareReleaseResponse__Output>
      PreparedPreviewSourceSnapshot: MessageTypeDefinition<_superverse_v2_PreparedPreviewSourceSnapshot, _superverse_v2_PreparedPreviewSourceSnapshot__Output>
      PreparedReleaseSourceSnapshot: MessageTypeDefinition<_superverse_v2_PreparedReleaseSourceSnapshot, _superverse_v2_PreparedReleaseSourceSnapshot__Output>
      PreviewDeploymentSnapshot: MessageTypeDefinition<_superverse_v2_PreviewDeploymentSnapshot, _superverse_v2_PreviewDeploymentSnapshot__Output>
      PreviewDeploymentStatus: EnumTypeDefinition
      PreviewService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_PreviewServiceClient> & { service: _superverse_v2_PreviewServiceDefinition }
      PreviewSnapshot: MessageTypeDefinition<_superverse_v2_PreviewSnapshot, _superverse_v2_PreviewSnapshot__Output>
      PreviewSourceStatus: EnumTypeDefinition
      PreviewStartRecord: MessageTypeDefinition<_superverse_v2_PreviewStartRecord, _superverse_v2_PreviewStartRecord__Output>
      PreviewStatus: EnumTypeDefinition
      PreviewValidation: MessageTypeDefinition<_superverse_v2_PreviewValidation, _superverse_v2_PreviewValidation__Output>
      ProjectConfigurationSnapshot: MessageTypeDefinition<_superverse_v2_ProjectConfigurationSnapshot, _superverse_v2_ProjectConfigurationSnapshot__Output>
      ProjectConfigurationTarget: MessageTypeDefinition<_superverse_v2_ProjectConfigurationTarget, _superverse_v2_ProjectConfigurationTarget__Output>
      ProjectConfigurationValidation: MessageTypeDefinition<_superverse_v2_ProjectConfigurationValidation, _superverse_v2_ProjectConfigurationValidation__Output>
      ProjectExample: MessageTypeDefinition<_superverse_v2_ProjectExample, _superverse_v2_ProjectExample__Output>
      ProjectLive: MessageTypeDefinition<_superverse_v2_ProjectLive, _superverse_v2_ProjectLive__Output>
      ProjectMemberService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_ProjectMemberServiceClient> & { service: _superverse_v2_ProjectMemberServiceDefinition }
      ProjectMemberSnapshot: MessageTypeDefinition<_superverse_v2_ProjectMemberSnapshot, _superverse_v2_ProjectMemberSnapshot__Output>
      ProjectRole: EnumTypeDefinition
      ProjectScope: EnumTypeDefinition
      ProjectService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_ProjectServiceClient> & { service: _superverse_v2_ProjectServiceDefinition }
      ProjectSnapshot: MessageTypeDefinition<_superverse_v2_ProjectSnapshot, _superverse_v2_ProjectSnapshot__Output>
      ProjectStatus: EnumTypeDefinition
      ReadFlowStreamRequest: MessageTypeDefinition<_superverse_v2_ReadFlowStreamRequest, _superverse_v2_ReadFlowStreamRequest__Output>
      ReadFlowStreamResponse: MessageTypeDefinition<_superverse_v2_ReadFlowStreamResponse, _superverse_v2_ReadFlowStreamResponse__Output>
      RecentTriggerEvent: MessageTypeDefinition<_superverse_v2_RecentTriggerEvent, _superverse_v2_RecentTriggerEvent__Output>
      RecoverFlowStartRequest: MessageTypeDefinition<_superverse_v2_RecoverFlowStartRequest, _superverse_v2_RecoverFlowStartRequest__Output>
      RecoverFlowStartResponse: MessageTypeDefinition<_superverse_v2_RecoverFlowStartResponse, _superverse_v2_RecoverFlowStartResponse__Output>
      RecoverReleaseBuildRequest: MessageTypeDefinition<_superverse_v2_RecoverReleaseBuildRequest, _superverse_v2_RecoverReleaseBuildRequest__Output>
      RecoverReleaseBuildResponse: MessageTypeDefinition<_superverse_v2_RecoverReleaseBuildResponse, _superverse_v2_RecoverReleaseBuildResponse__Output>
      RegisterAccountRequest: MessageTypeDefinition<_superverse_v2_RegisterAccountRequest, _superverse_v2_RegisterAccountRequest__Output>
      RegisterAccountResponse: MessageTypeDefinition<_superverse_v2_RegisterAccountResponse, _superverse_v2_RegisterAccountResponse__Output>
      ReleaseBuildRevisionSnapshot: MessageTypeDefinition<_superverse_v2_ReleaseBuildRevisionSnapshot, _superverse_v2_ReleaseBuildRevisionSnapshot__Output>
      ReleaseCandidate: MessageTypeDefinition<_superverse_v2_ReleaseCandidate, _superverse_v2_ReleaseCandidate__Output>
      ReleaseService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_ReleaseServiceClient> & { service: _superverse_v2_ReleaseServiceDefinition }
      ReleaseSnapshot: MessageTypeDefinition<_superverse_v2_ReleaseSnapshot, _superverse_v2_ReleaseSnapshot__Output>
      ReleaseStatus: EnumTypeDefinition
      ReleaseUnusedConnectionsRequest: MessageTypeDefinition<_superverse_v2_ReleaseUnusedConnectionsRequest, _superverse_v2_ReleaseUnusedConnectionsRequest__Output>
      ReleaseUnusedConnectionsResponse: MessageTypeDefinition<_superverse_v2_ReleaseUnusedConnectionsResponse, _superverse_v2_ReleaseUnusedConnectionsResponse__Output>
      RemoteAgentBinding: MessageTypeDefinition<_superverse_v2_RemoteAgentBinding, _superverse_v2_RemoteAgentBinding__Output>
      RemoteAgentConnection: MessageTypeDefinition<_superverse_v2_RemoteAgentConnection, _superverse_v2_RemoteAgentConnection__Output>
      RemoteAgentConnectionStatus: EnumTypeDefinition
      RemoteAgentService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_RemoteAgentServiceClient> & { service: _superverse_v2_RemoteAgentServiceDefinition }
      RemoteSessionExecutor: MessageTypeDefinition<_superverse_v2_RemoteSessionExecutor, _superverse_v2_RemoteSessionExecutor__Output>
      RemoveLiveRequest: MessageTypeDefinition<_superverse_v2_RemoveLiveRequest, _superverse_v2_RemoveLiveRequest__Output>
      RemoveLiveResponse: MessageTypeDefinition<_superverse_v2_RemoveLiveResponse, _superverse_v2_RemoveLiveResponse__Output>
      RemoveProjectMemberRequest: MessageTypeDefinition<_superverse_v2_RemoveProjectMemberRequest, _superverse_v2_RemoveProjectMemberRequest__Output>
      RemoveProjectMemberResponse: MessageTypeDefinition<_superverse_v2_RemoveProjectMemberResponse, _superverse_v2_RemoveProjectMemberResponse__Output>
      RemoveTeamSpaceMemberRequest: MessageTypeDefinition<_superverse_v2_RemoveTeamSpaceMemberRequest, _superverse_v2_RemoveTeamSpaceMemberRequest__Output>
      RemoveTeamSpaceMemberResponse: MessageTypeDefinition<_superverse_v2_RemoveTeamSpaceMemberResponse, _superverse_v2_RemoveTeamSpaceMemberResponse__Output>
      RenameTeamSpaceRequest: MessageTypeDefinition<_superverse_v2_RenameTeamSpaceRequest, _superverse_v2_RenameTeamSpaceRequest__Output>
      RenameTeamSpaceResponse: MessageTypeDefinition<_superverse_v2_RenameTeamSpaceResponse, _superverse_v2_RenameTeamSpaceResponse__Output>
      RenewWorkspaceRequest: MessageTypeDefinition<_superverse_v2_RenewWorkspaceRequest, _superverse_v2_RenewWorkspaceRequest__Output>
      RenewWorkspaceResponse: MessageTypeDefinition<_superverse_v2_RenewWorkspaceResponse, _superverse_v2_RenewWorkspaceResponse__Output>
      Repository: MessageTypeDefinition<_superverse_v2_Repository, _superverse_v2_Repository__Output>
      ResolveToolApprovalRequest: MessageTypeDefinition<_superverse_v2_ResolveToolApprovalRequest, _superverse_v2_ResolveToolApprovalRequest__Output>
      ResolveToolApprovalResponse: MessageTypeDefinition<_superverse_v2_ResolveToolApprovalResponse, _superverse_v2_ResolveToolApprovalResponse__Output>
      ResourceProgress: MessageTypeDefinition<_superverse_v2_ResourceProgress, _superverse_v2_ResourceProgress__Output>
      RevokeRemoteAgentConnectionRequest: MessageTypeDefinition<_superverse_v2_RevokeRemoteAgentConnectionRequest, _superverse_v2_RevokeRemoteAgentConnectionRequest__Output>
      RevokeRemoteAgentConnectionResponse: MessageTypeDefinition<_superverse_v2_RevokeRemoteAgentConnectionResponse, _superverse_v2_RevokeRemoteAgentConnectionResponse__Output>
      SaveApplicationEnvironmentRequest: MessageTypeDefinition<_superverse_v2_SaveApplicationEnvironmentRequest, _superverse_v2_SaveApplicationEnvironmentRequest__Output>
      SaveApplicationEnvironmentResponse: MessageTypeDefinition<_superverse_v2_SaveApplicationEnvironmentResponse, _superverse_v2_SaveApplicationEnvironmentResponse__Output>
      SaveOperationConfigurationRequest: MessageTypeDefinition<_superverse_v2_SaveOperationConfigurationRequest, _superverse_v2_SaveOperationConfigurationRequest__Output>
      SaveOperationConfigurationResponse: MessageTypeDefinition<_superverse_v2_SaveOperationConfigurationResponse, _superverse_v2_SaveOperationConfigurationResponse__Output>
      SaveTriggerBindingRequest: MessageTypeDefinition<_superverse_v2_SaveTriggerBindingRequest, _superverse_v2_SaveTriggerBindingRequest__Output>
      SaveTriggerBindingResponse: MessageTypeDefinition<_superverse_v2_SaveTriggerBindingResponse, _superverse_v2_SaveTriggerBindingResponse__Output>
      SearchFlowsRequest: MessageTypeDefinition<_superverse_v2_SearchFlowsRequest, _superverse_v2_SearchFlowsRequest__Output>
      SearchFlowsResponse: MessageTypeDefinition<_superverse_v2_SearchFlowsResponse, _superverse_v2_SearchFlowsResponse__Output>
      SelectTeamSpaceConnectionRequest: MessageTypeDefinition<_superverse_v2_SelectTeamSpaceConnectionRequest, _superverse_v2_SelectTeamSpaceConnectionRequest__Output>
      SelectTeamSpaceConnectionResponse: MessageTypeDefinition<_superverse_v2_SelectTeamSpaceConnectionResponse, _superverse_v2_SelectTeamSpaceConnectionResponse__Output>
      SendMessageRequest: MessageTypeDefinition<_superverse_v2_SendMessageRequest, _superverse_v2_SendMessageRequest__Output>
      SendMessageResponse: MessageTypeDefinition<_superverse_v2_SendMessageResponse, _superverse_v2_SendMessageResponse__Output>
      SessionExecutor: MessageTypeDefinition<_superverse_v2_SessionExecutor, _superverse_v2_SessionExecutor__Output>
      SessionExecutorMode: EnumTypeDefinition
      SessionProgress: MessageTypeDefinition<_superverse_v2_SessionProgress, _superverse_v2_SessionProgress__Output>
      SessionService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_SessionServiceClient> & { service: _superverse_v2_SessionServiceDefinition }
      SessionSnapshot: MessageTypeDefinition<_superverse_v2_SessionSnapshot, _superverse_v2_SessionSnapshot__Output>
      SessionStatus: EnumTypeDefinition
      SessionWorkspaceSnapshot: MessageTypeDefinition<_superverse_v2_SessionWorkspaceSnapshot, _superverse_v2_SessionWorkspaceSnapshot__Output>
      SetModelCredentialRequest: MessageTypeDefinition<_superverse_v2_SetModelCredentialRequest, _superverse_v2_SetModelCredentialRequest__Output>
      SetModelCredentialResponse: MessageTypeDefinition<_superverse_v2_SetModelCredentialResponse, _superverse_v2_SetModelCredentialResponse__Output>
      SetProjectMemberRoleRequest: MessageTypeDefinition<_superverse_v2_SetProjectMemberRoleRequest, _superverse_v2_SetProjectMemberRoleRequest__Output>
      SetProjectMemberRoleResponse: MessageTypeDefinition<_superverse_v2_SetProjectMemberRoleResponse, _superverse_v2_SetProjectMemberRoleResponse__Output>
      SetSessionExecutorRequest: MessageTypeDefinition<_superverse_v2_SetSessionExecutorRequest, _superverse_v2_SetSessionExecutorRequest__Output>
      SetSessionExecutorResponse: MessageTypeDefinition<_superverse_v2_SetSessionExecutorResponse, _superverse_v2_SetSessionExecutorResponse__Output>
      SetTeamSpaceMemberRoleRequest: MessageTypeDefinition<_superverse_v2_SetTeamSpaceMemberRoleRequest, _superverse_v2_SetTeamSpaceMemberRoleRequest__Output>
      SetTeamSpaceMemberRoleResponse: MessageTypeDefinition<_superverse_v2_SetTeamSpaceMemberRoleResponse, _superverse_v2_SetTeamSpaceMemberRoleResponse__Output>
      SetupSnapshot: MessageTypeDefinition<_superverse_v2_SetupSnapshot, _superverse_v2_SetupSnapshot__Output>
      SetupStatus: EnumTypeDefinition
      StartConnectorUiSessionRequest: MessageTypeDefinition<_superverse_v2_StartConnectorUiSessionRequest, _superverse_v2_StartConnectorUiSessionRequest__Output>
      StartConnectorUiSessionResponse: MessageTypeDefinition<_superverse_v2_StartConnectorUiSessionResponse, _superverse_v2_StartConnectorUiSessionResponse__Output>
      StartDeploymentRequest: MessageTypeDefinition<_superverse_v2_StartDeploymentRequest, _superverse_v2_StartDeploymentRequest__Output>
      StartDeploymentResponse: MessageTypeDefinition<_superverse_v2_StartDeploymentResponse, _superverse_v2_StartDeploymentResponse__Output>
      StartEvolveReviewRequest: MessageTypeDefinition<_superverse_v2_StartEvolveReviewRequest, _superverse_v2_StartEvolveReviewRequest__Output>
      StartEvolveReviewResponse: MessageTypeDefinition<_superverse_v2_StartEvolveReviewResponse, _superverse_v2_StartEvolveReviewResponse__Output>
      StartFlowRequest: MessageTypeDefinition<_superverse_v2_StartFlowRequest, _superverse_v2_StartFlowRequest__Output>
      StartFlowResponse: MessageTypeDefinition<_superverse_v2_StartFlowResponse, _superverse_v2_StartFlowResponse__Output>
      StartOutcome: EnumTypeDefinition
      StartPreviewRequest: MessageTypeDefinition<_superverse_v2_StartPreviewRequest, _superverse_v2_StartPreviewRequest__Output>
      StartPreviewResponse: MessageTypeDefinition<_superverse_v2_StartPreviewResponse, _superverse_v2_StartPreviewResponse__Output>
      StartReleaseBuildRequest: MessageTypeDefinition<_superverse_v2_StartReleaseBuildRequest, _superverse_v2_StartReleaseBuildRequest__Output>
      StartReleaseBuildResponse: MessageTypeDefinition<_superverse_v2_StartReleaseBuildResponse, _superverse_v2_StartReleaseBuildResponse__Output>
      StartTeamSpaceConnectionOAuthRequest: MessageTypeDefinition<_superverse_v2_StartTeamSpaceConnectionOAuthRequest, _superverse_v2_StartTeamSpaceConnectionOAuthRequest__Output>
      StartTeamSpaceConnectionOAuthResponse: MessageTypeDefinition<_superverse_v2_StartTeamSpaceConnectionOAuthResponse, _superverse_v2_StartTeamSpaceConnectionOAuthResponse__Output>
      StartTeamSpaceConnectionUiSessionRequest: MessageTypeDefinition<_superverse_v2_StartTeamSpaceConnectionUiSessionRequest, _superverse_v2_StartTeamSpaceConnectionUiSessionRequest__Output>
      StartTeamSpaceConnectionUiSessionResponse: MessageTypeDefinition<_superverse_v2_StartTeamSpaceConnectionUiSessionResponse, _superverse_v2_StartTeamSpaceConnectionUiSessionResponse__Output>
      SteerQueuedMessageRequest: MessageTypeDefinition<_superverse_v2_SteerQueuedMessageRequest, _superverse_v2_SteerQueuedMessageRequest__Output>
      SteerQueuedMessageResponse: MessageTypeDefinition<_superverse_v2_SteerQueuedMessageResponse, _superverse_v2_SteerQueuedMessageResponse__Output>
      StopFlowRequest: MessageTypeDefinition<_superverse_v2_StopFlowRequest, _superverse_v2_StopFlowRequest__Output>
      StopFlowResponse: MessageTypeDefinition<_superverse_v2_StopFlowResponse, _superverse_v2_StopFlowResponse__Output>
      StopPreviewRequest: MessageTypeDefinition<_superverse_v2_StopPreviewRequest, _superverse_v2_StopPreviewRequest__Output>
      StopPreviewResponse: MessageTypeDefinition<_superverse_v2_StopPreviewResponse, _superverse_v2_StopPreviewResponse__Output>
      TeamSpaceConnectionService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_TeamSpaceConnectionServiceClient> & { service: _superverse_v2_TeamSpaceConnectionServiceDefinition }
      TeamSpaceConnectionSnapshot: MessageTypeDefinition<_superverse_v2_TeamSpaceConnectionSnapshot, _superverse_v2_TeamSpaceConnectionSnapshot__Output>
      TeamSpaceConnectionStatus: EnumTypeDefinition
      TeamSpaceConnectionUsage: MessageTypeDefinition<_superverse_v2_TeamSpaceConnectionUsage, _superverse_v2_TeamSpaceConnectionUsage__Output>
      TeamSpaceMemberService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_TeamSpaceMemberServiceClient> & { service: _superverse_v2_TeamSpaceMemberServiceDefinition }
      TeamSpaceMemberSnapshot: MessageTypeDefinition<_superverse_v2_TeamSpaceMemberSnapshot, _superverse_v2_TeamSpaceMemberSnapshot__Output>
      TeamSpaceOverview: MessageTypeDefinition<_superverse_v2_TeamSpaceOverview, _superverse_v2_TeamSpaceOverview__Output>
      TeamSpaceRole: EnumTypeDefinition
      TeamSpaceService: SubtypeConstructor<typeof grpc.Client, _superverse_v2_TeamSpaceServiceClient> & { service: _superverse_v2_TeamSpaceServiceDefinition }
      TeamSpaceSnapshot: MessageTypeDefinition<_superverse_v2_TeamSpaceSnapshot, _superverse_v2_TeamSpaceSnapshot__Output>
      TeamSpaceStatus: EnumTypeDefinition
      TimeTravelFlowRequest: MessageTypeDefinition<_superverse_v2_TimeTravelFlowRequest, _superverse_v2_TimeTravelFlowRequest__Output>
      TimeTravelFlowResponse: MessageTypeDefinition<_superverse_v2_TimeTravelFlowResponse, _superverse_v2_TimeTravelFlowResponse__Output>
      UnroutedEvents: MessageTypeDefinition<_superverse_v2_UnroutedEvents, _superverse_v2_UnroutedEvents__Output>
      UpdateFlowDisplayFieldRequest: MessageTypeDefinition<_superverse_v2_UpdateFlowDisplayFieldRequest, _superverse_v2_UpdateFlowDisplayFieldRequest__Output>
      UpdateFlowDisplayFieldResponse: MessageTypeDefinition<_superverse_v2_UpdateFlowDisplayFieldResponse, _superverse_v2_UpdateFlowDisplayFieldResponse__Output>
      UpdatePreviewAutoDeployRequest: MessageTypeDefinition<_superverse_v2_UpdatePreviewAutoDeployRequest, _superverse_v2_UpdatePreviewAutoDeployRequest__Output>
      UpdatePreviewAutoDeployResponse: MessageTypeDefinition<_superverse_v2_UpdatePreviewAutoDeployResponse, _superverse_v2_UpdatePreviewAutoDeployResponse__Output>
      UpdateSessionModelRequest: MessageTypeDefinition<_superverse_v2_UpdateSessionModelRequest, _superverse_v2_UpdateSessionModelRequest__Output>
      UpdateSessionModelResponse: MessageTypeDefinition<_superverse_v2_UpdateSessionModelResponse, _superverse_v2_UpdateSessionModelResponse__Output>
      UpdateTeamSpaceConnectionRequest: MessageTypeDefinition<_superverse_v2_UpdateTeamSpaceConnectionRequest, _superverse_v2_UpdateTeamSpaceConnectionRequest__Output>
      UpdateTeamSpaceConnectionResponse: MessageTypeDefinition<_superverse_v2_UpdateTeamSpaceConnectionResponse, _superverse_v2_UpdateTeamSpaceConnectionResponse__Output>
      UploadHeader: MessageTypeDefinition<_superverse_v2_UploadHeader, _superverse_v2_UploadHeader__Output>
      ValidateProjectConfigurationRequest: MessageTypeDefinition<_superverse_v2_ValidateProjectConfigurationRequest, _superverse_v2_ValidateProjectConfigurationRequest__Output>
      ValidateProjectConfigurationResponse: MessageTypeDefinition<_superverse_v2_ValidateProjectConfigurationResponse, _superverse_v2_ValidateProjectConfigurationResponse__Output>
      WaitFlowHistoryRequest: MessageTypeDefinition<_superverse_v2_WaitFlowHistoryRequest, _superverse_v2_WaitFlowHistoryRequest__Output>
      WaitFlowHistoryResponse: MessageTypeDefinition<_superverse_v2_WaitFlowHistoryResponse, _superverse_v2_WaitFlowHistoryResponse__Output>
      WatchDeploymentRequest: MessageTypeDefinition<_superverse_v2_WatchDeploymentRequest, _superverse_v2_WatchDeploymentRequest__Output>
      WatchDeploymentResponse: MessageTypeDefinition<_superverse_v2_WatchDeploymentResponse, _superverse_v2_WatchDeploymentResponse__Output>
      WatchProjectRequest: MessageTypeDefinition<_superverse_v2_WatchProjectRequest, _superverse_v2_WatchProjectRequest__Output>
      WatchProjectResponse: MessageTypeDefinition<_superverse_v2_WatchProjectResponse, _superverse_v2_WatchProjectResponse__Output>
      WatchReleaseRequest: MessageTypeDefinition<_superverse_v2_WatchReleaseRequest, _superverse_v2_WatchReleaseRequest__Output>
      WatchReleaseResponse: MessageTypeDefinition<_superverse_v2_WatchReleaseResponse, _superverse_v2_WatchReleaseResponse__Output>
      WatchSessionRequest: MessageTypeDefinition<_superverse_v2_WatchSessionRequest, _superverse_v2_WatchSessionRequest__Output>
      WatchSessionResponse: MessageTypeDefinition<_superverse_v2_WatchSessionResponse, _superverse_v2_WatchSessionResponse__Output>
      WorkspaceState: EnumTypeDefinition
      WorkspaceStatus: EnumTypeDefinition
    }
  }
}
