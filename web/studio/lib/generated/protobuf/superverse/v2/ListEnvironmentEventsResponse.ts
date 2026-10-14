// Original file: api/superverse/v2/superverse.proto

import type { EnvironmentEvent as _superverse_v2_EnvironmentEvent, EnvironmentEvent__Output as _superverse_v2_EnvironmentEvent__Output } from '../../superverse/v2/EnvironmentEvent';

export interface ListEnvironmentEventsResponse {
  'events'?: (_superverse_v2_EnvironmentEvent)[];
  'nextPageToken'?: (string);
}

export interface ListEnvironmentEventsResponse__Output {
  'events': (_superverse_v2_EnvironmentEvent__Output)[];
  'nextPageToken': (string);
}
