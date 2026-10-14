// Original file: api/superverse/v2/superverse.proto

import type { SessionStatus as _superverse_v2_SessionStatus, SessionStatus__Output as _superverse_v2_SessionStatus__Output } from '../../superverse/v2/SessionStatus';
import type { AgentSnapshot as _superverse_v2_AgentSnapshot, AgentSnapshot__Output as _superverse_v2_AgentSnapshot__Output } from '../../superverse/v2/AgentSnapshot';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { SessionWorkspaceSnapshot as _superverse_v2_SessionWorkspaceSnapshot, SessionWorkspaceSnapshot__Output as _superverse_v2_SessionWorkspaceSnapshot__Output } from '../../superverse/v2/SessionWorkspaceSnapshot';
import type { PreviewSnapshot as _superverse_v2_PreviewSnapshot, PreviewSnapshot__Output as _superverse_v2_PreviewSnapshot__Output } from '../../superverse/v2/PreviewSnapshot';
import type { ConnectorContribution as _superverse_v2_ConnectorContribution, ConnectorContribution__Output as _superverse_v2_ConnectorContribution__Output } from '../../superverse/v2/ConnectorContribution';
import type { Long } from '@grpc/proto-loader';

export interface SessionSnapshot {
  'projectId'?: (string);
  'status'?: (_superverse_v2_SessionStatus);
  'revision'?: (number | string | Long);
  'headCommit'?: (string);
  'agent'?: (_superverse_v2_AgentSnapshot | null);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'failure'?: (_superverse_v2_Failure | null);
  'updatedAt'?: (_google_protobuf_Timestamp | null);
  'workspace'?: (_superverse_v2_SessionWorkspaceSnapshot | null);
  'preview'?: (_superverse_v2_PreviewSnapshot | null);
  'connectorContributions'?: (_superverse_v2_ConnectorContribution)[];
}

export interface SessionSnapshot__Output {
  'projectId': (string);
  'status': (_superverse_v2_SessionStatus__Output);
  'revision': (string);
  'headCommit': (string);
  'agent': (_superverse_v2_AgentSnapshot__Output | null);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'failure': (_superverse_v2_Failure__Output | null);
  'updatedAt': (_google_protobuf_Timestamp__Output | null);
  'workspace': (_superverse_v2_SessionWorkspaceSnapshot__Output | null);
  'preview': (_superverse_v2_PreviewSnapshot__Output | null);
  'connectorContributions': (_superverse_v2_ConnectorContribution__Output)[];
}
