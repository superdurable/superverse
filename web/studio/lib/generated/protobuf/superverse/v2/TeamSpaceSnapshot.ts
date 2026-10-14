// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceStatus as _superverse_v2_TeamSpaceStatus, TeamSpaceStatus__Output as _superverse_v2_TeamSpaceStatus__Output } from '../../superverse/v2/TeamSpaceStatus';
import type { TeamSpaceRole as _superverse_v2_TeamSpaceRole, TeamSpaceRole__Output as _superverse_v2_TeamSpaceRole__Output } from '../../superverse/v2/TeamSpaceRole';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Long } from '@grpc/proto-loader';

export interface TeamSpaceSnapshot {
  'teamSpaceId'?: (string);
  'name'?: (string);
  'status'?: (_superverse_v2_TeamSpaceStatus);
  'revision'?: (number | string | Long);
  'creatorAccountId'?: (string);
  'callerRole'?: (_superverse_v2_TeamSpaceRole);
  'failure'?: (_superverse_v2_Failure | null);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'updatedAt'?: (_google_protobuf_Timestamp | null);
}

export interface TeamSpaceSnapshot__Output {
  'teamSpaceId': (string);
  'name': (string);
  'status': (_superverse_v2_TeamSpaceStatus__Output);
  'revision': (string);
  'creatorAccountId': (string);
  'callerRole': (_superverse_v2_TeamSpaceRole__Output);
  'failure': (_superverse_v2_Failure__Output | null);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'updatedAt': (_google_protobuf_Timestamp__Output | null);
}
