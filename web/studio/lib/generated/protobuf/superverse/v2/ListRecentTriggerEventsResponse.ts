// Original file: api/superverse/v2/superverse.proto

import type { RecentTriggerEvent as _superverse_v2_RecentTriggerEvent, RecentTriggerEvent__Output as _superverse_v2_RecentTriggerEvent__Output } from '../../superverse/v2/RecentTriggerEvent';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface ListRecentTriggerEventsResponse {
  'events'?: (_superverse_v2_RecentTriggerEvent)[];
  'bufferStartedAt'?: (_google_protobuf_Timestamp | null);
  'bufferInstanceId'?: (string);
}

export interface ListRecentTriggerEventsResponse__Output {
  'events': (_superverse_v2_RecentTriggerEvent__Output)[];
  'bufferStartedAt': (_google_protobuf_Timestamp__Output | null);
  'bufferInstanceId': (string);
}
