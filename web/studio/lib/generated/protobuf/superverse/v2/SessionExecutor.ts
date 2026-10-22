// Original file: api/superverse/v2/superverse.proto

import type { SessionExecutorMode as _superverse_v2_SessionExecutorMode, SessionExecutorMode__Output as _superverse_v2_SessionExecutorMode__Output } from '../../superverse/v2/SessionExecutorMode';
import type { RemoteSessionExecutor as _superverse_v2_RemoteSessionExecutor, RemoteSessionExecutor__Output as _superverse_v2_RemoteSessionExecutor__Output } from '../../superverse/v2/RemoteSessionExecutor';
import type { Long } from '@grpc/proto-loader';

export interface SessionExecutor {
  'mode'?: (_superverse_v2_SessionExecutorMode);
  'epoch'?: (number | string | Long);
  'generation'?: (number | string | Long);
  'remote'?: (_superverse_v2_RemoteSessionExecutor | null);
  'suspended'?: (_superverse_v2_RemoteSessionExecutor | null);
}

export interface SessionExecutor__Output {
  'mode': (_superverse_v2_SessionExecutorMode__Output);
  'epoch': (string);
  'generation': (string);
  'remote': (_superverse_v2_RemoteSessionExecutor__Output | null);
  'suspended': (_superverse_v2_RemoteSessionExecutor__Output | null);
}
