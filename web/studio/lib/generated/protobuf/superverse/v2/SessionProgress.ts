// Original file: api/superverse/v2/superverse.proto

import type { AgentEvent as _superverse_v2_AgentEvent, AgentEvent__Output as _superverse_v2_AgentEvent__Output } from '../../superverse/v2/AgentEvent';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface SessionProgress {
  'activity'?: (_superverse_v2_AgentEvent | null);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'source'?: (string);
  'reasoningSummary'?: (string);
  'assistantText'?: (string);
  'taskId'?: (string);
  'content'?: "activity"|"reasoningSummary"|"assistantText";
}

export interface SessionProgress__Output {
  'activity'?: (_superverse_v2_AgentEvent__Output | null);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'source': (string);
  'reasoningSummary'?: (string);
  'assistantText'?: (string);
  'taskId': (string);
  'content'?: "activity"|"reasoningSummary"|"assistantText";
}
