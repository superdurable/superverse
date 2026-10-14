// Original file: api/superverse/v2/superverse.proto

import type { ConfigurationItemKey as _superverse_v2_ConfigurationItemKey, ConfigurationItemKey__Output as _superverse_v2_ConfigurationItemKey__Output } from '../../superverse/v2/ConfigurationItemKey';
import type { ConfigurationItemWarning as _superverse_v2_ConfigurationItemWarning, ConfigurationItemWarning__Output as _superverse_v2_ConfigurationItemWarning__Output } from '../../superverse/v2/ConfigurationItemWarning';
import type { ConfigurationFollowRefusal as _superverse_v2_ConfigurationFollowRefusal, ConfigurationFollowRefusal__Output as _superverse_v2_ConfigurationFollowRefusal__Output } from '../../superverse/v2/ConfigurationFollowRefusal';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_ConfigurationItem_State = {
  STATE_UNSPECIFIED: 'STATE_UNSPECIFIED',
  STATE_UNCHANGED: 'STATE_UNCHANGED',
  STATE_CHANGED: 'STATE_CHANGED',
  STATE_ADDED: 'STATE_ADDED',
  STATE_REMOVED: 'STATE_REMOVED',
  STATE_NO_LONGER_USED: 'STATE_NO_LONGER_USED',
  STATE_FOLLOWS_PREVIEW: 'STATE_FOLLOWS_PREVIEW',
  STATE_OWN: 'STATE_OWN',
} as const;

export type _superverse_v2_ConfigurationItem_State =
  | 'STATE_UNSPECIFIED'
  | 0
  | 'STATE_UNCHANGED'
  | 1
  | 'STATE_CHANGED'
  | 2
  | 'STATE_ADDED'
  | 3
  | 'STATE_REMOVED'
  | 4
  | 'STATE_NO_LONGER_USED'
  | 8
  | 'STATE_FOLLOWS_PREVIEW'
  | 10
  | 'STATE_OWN'
  | 11

export type _superverse_v2_ConfigurationItem_State__Output = typeof _superverse_v2_ConfigurationItem_State[keyof typeof _superverse_v2_ConfigurationItem_State]

export interface ConfigurationItem {
  'key'?: (_superverse_v2_ConfigurationItemKey | null);
  'state'?: (_superverse_v2_ConfigurationItem_State);
  'differsFromLive'?: (boolean);
  'differsFromPreview'?: (boolean);
  'warnings'?: (_superverse_v2_ConfigurationItemWarning)[];
  'warningProjectName'?: (string);
  'liveValueJson'?: (string);
  'previewValueJson'?: (string);
  'followRefusal'?: (_superverse_v2_ConfigurationFollowRefusal);
}

export interface ConfigurationItem__Output {
  'key': (_superverse_v2_ConfigurationItemKey__Output | null);
  'state': (_superverse_v2_ConfigurationItem_State__Output);
  'differsFromLive': (boolean);
  'differsFromPreview': (boolean);
  'warnings': (_superverse_v2_ConfigurationItemWarning__Output)[];
  'warningProjectName': (string);
  'liveValueJson': (string);
  'previewValueJson': (string);
  'followRefusal': (_superverse_v2_ConfigurationFollowRefusal__Output);
}
