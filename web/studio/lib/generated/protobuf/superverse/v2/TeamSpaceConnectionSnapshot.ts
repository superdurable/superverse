// Original file: api/superverse/v2/superverse.proto

import type { ConnectionClientSource as _superverse_v2_ConnectionClientSource, ConnectionClientSource__Output as _superverse_v2_ConnectionClientSource__Output } from '../../superverse/v2/ConnectionClientSource';
import type { CredentialStatus as _superverse_v2_CredentialStatus, CredentialStatus__Output as _superverse_v2_CredentialStatus__Output } from '../../superverse/v2/CredentialStatus';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { TeamSpaceConnectionStatus as _superverse_v2_TeamSpaceConnectionStatus, TeamSpaceConnectionStatus__Output as _superverse_v2_TeamSpaceConnectionStatus__Output } from '../../superverse/v2/TeamSpaceConnectionStatus';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { TeamSpaceConnectionUsage as _superverse_v2_TeamSpaceConnectionUsage, TeamSpaceConnectionUsage__Output as _superverse_v2_TeamSpaceConnectionUsage__Output } from '../../superverse/v2/TeamSpaceConnectionUsage';
import type { UnroutedEvents as _superverse_v2_UnroutedEvents, UnroutedEvents__Output as _superverse_v2_UnroutedEvents__Output } from '../../superverse/v2/UnroutedEvents';
import type { Long } from '@grpc/proto-loader';

export interface TeamSpaceConnectionSnapshot {
  'teamSpaceId'?: (string);
  'connectionId'?: (string);
  'name'?: (string);
  'connectorId'?: (string);
  'modulePath'?: (string);
  'versionLine'?: (string);
  'manifestVersion'?: (string);
  'authMethodId'?: (string);
  'clientSource'?: (_superverse_v2_ConnectionClientSource);
  'settingsJson'?: (string);
  'revision'?: (number | string | Long);
  'credentialStatus'?: (_superverse_v2_CredentialStatus);
  'credentialRevision'?: (number | string | Long);
  'credentialIssuedAt'?: (_google_protobuf_Timestamp | null);
  'credentialExpiresAt'?: (_google_protobuf_Timestamp | null);
  'status'?: (_superverse_v2_TeamSpaceConnectionStatus);
  'lastFailure'?: (_superverse_v2_Failure | null);
  'creatorAccountId'?: (string);
  'updatedByAccountId'?: (string);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'updatedAt'?: (_google_protobuf_Timestamp | null);
  'usages'?: (_superverse_v2_TeamSpaceConnectionUsage)[];
  'unroutedEvents'?: (_superverse_v2_UnroutedEvents | null);
}

export interface TeamSpaceConnectionSnapshot__Output {
  'teamSpaceId': (string);
  'connectionId': (string);
  'name': (string);
  'connectorId': (string);
  'modulePath': (string);
  'versionLine': (string);
  'manifestVersion': (string);
  'authMethodId': (string);
  'clientSource': (_superverse_v2_ConnectionClientSource__Output);
  'settingsJson': (string);
  'revision': (string);
  'credentialStatus': (_superverse_v2_CredentialStatus__Output);
  'credentialRevision': (string);
  'credentialIssuedAt': (_google_protobuf_Timestamp__Output | null);
  'credentialExpiresAt': (_google_protobuf_Timestamp__Output | null);
  'status': (_superverse_v2_TeamSpaceConnectionStatus__Output);
  'lastFailure': (_superverse_v2_Failure__Output | null);
  'creatorAccountId': (string);
  'updatedByAccountId': (string);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'updatedAt': (_google_protobuf_Timestamp__Output | null);
  'usages': (_superverse_v2_TeamSpaceConnectionUsage__Output)[];
  'unroutedEvents': (_superverse_v2_UnroutedEvents__Output | null);
}
