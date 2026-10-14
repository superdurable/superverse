// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

export interface AgentModelCallMetrics {
  'model'?: (string);
  'effort'?: (string);
  'durationMs'?: (number | string | Long);
  'firstEventMs'?: (number | string | Long);
  'inputTokens'?: (number | string | Long);
  'cacheReadInputTokens'?: (number | string | Long);
  'outputTokens'?: (number | string | Long);
  'thinkingTokens'?: (number | string | Long);
  'cacheCreationInputTokens'?: (number | string | Long);
  'droppedThinkingBlocks'?: (number | string | Long);
  'servedModel'?: (string);
  '_firstEventMs'?: "firstEventMs";
  '_inputTokens'?: "inputTokens";
  '_cacheReadInputTokens'?: "cacheReadInputTokens";
  '_outputTokens'?: "outputTokens";
  '_thinkingTokens'?: "thinkingTokens";
  '_cacheCreationInputTokens'?: "cacheCreationInputTokens";
}

export interface AgentModelCallMetrics__Output {
  'model': (string);
  'effort': (string);
  'durationMs': (string);
  'firstEventMs'?: (string);
  'inputTokens'?: (string);
  'cacheReadInputTokens'?: (string);
  'outputTokens'?: (string);
  'thinkingTokens'?: (string);
  'cacheCreationInputTokens'?: (string);
  'droppedThinkingBlocks': (string);
  'servedModel': (string);
  '_firstEventMs'?: "firstEventMs";
  '_inputTokens'?: "inputTokens";
  '_cacheReadInputTokens'?: "cacheReadInputTokens";
  '_outputTokens'?: "outputTokens";
  '_thinkingTokens'?: "thinkingTokens";
  '_cacheCreationInputTokens'?: "cacheCreationInputTokens";
}
