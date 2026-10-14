// Original file: api/superverse/v2/superverse.proto

import type { NewTeamSpaceConnection as _superverse_v2_NewTeamSpaceConnection, NewTeamSpaceConnection__Output as _superverse_v2_NewTeamSpaceConnection__Output } from '../../superverse/v2/NewTeamSpaceConnection';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface StartTeamSpaceConnectionOAuthRequest {
  'teamSpaceId'?: (string);
  'connectionId'?: (string);
  'version'?: (string);
  'clientId'?: (string);
  'clientSecret'?: (string);
  'credentialValuesJson'?: (string);
  'credentialSecretsJson'?: (string);
  'newConnection'?: (_superverse_v2_NewTeamSpaceConnection | null);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface StartTeamSpaceConnectionOAuthRequest__Output {
  'teamSpaceId': (string);
  'connectionId': (string);
  'version': (string);
  'clientId': (string);
  'clientSecret': (string);
  'credentialValuesJson': (string);
  'credentialSecretsJson': (string);
  'newConnection': (_superverse_v2_NewTeamSpaceConnection__Output | null);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
