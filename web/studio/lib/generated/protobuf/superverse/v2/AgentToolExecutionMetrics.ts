// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentToolExecutionMetrics_Outcome = {
  OUTCOME_UNSPECIFIED: 'OUTCOME_UNSPECIFIED',
  OUTCOME_SUCCEEDED: 'OUTCOME_SUCCEEDED',
  OUTCOME_KNOWN_FAILURE: 'OUTCOME_KNOWN_FAILURE',
  OUTCOME_UNKNOWN: 'OUTCOME_UNKNOWN',
} as const;

export type _superverse_v2_AgentToolExecutionMetrics_Outcome =
  | 'OUTCOME_UNSPECIFIED'
  | 0
  | 'OUTCOME_SUCCEEDED'
  | 1
  | 'OUTCOME_KNOWN_FAILURE'
  | 2
  | 'OUTCOME_UNKNOWN'
  | 3

export type _superverse_v2_AgentToolExecutionMetrics_Outcome__Output = typeof _superverse_v2_AgentToolExecutionMetrics_Outcome[keyof typeof _superverse_v2_AgentToolExecutionMetrics_Outcome]

export interface AgentToolExecutionMetrics {
  'outcome'?: (_superverse_v2_AgentToolExecutionMetrics_Outcome);
  'durationMs'?: (number | string | Long);
  'exitCode'?: (number);
  'resultBytes'?: (number | string | Long);
  'omittedBytes'?: (number | string | Long);
  'resultCode'?: (string);
  '_durationMs'?: "durationMs";
  '_exitCode'?: "exitCode";
  '_omittedBytes'?: "omittedBytes";
}

export interface AgentToolExecutionMetrics__Output {
  'outcome': (_superverse_v2_AgentToolExecutionMetrics_Outcome__Output);
  'durationMs'?: (string);
  'exitCode'?: (number);
  'resultBytes': (string);
  'omittedBytes'?: (string);
  'resultCode': (string);
  '_durationMs'?: "durationMs";
  '_exitCode'?: "exitCode";
  '_omittedBytes'?: "omittedBytes";
}
