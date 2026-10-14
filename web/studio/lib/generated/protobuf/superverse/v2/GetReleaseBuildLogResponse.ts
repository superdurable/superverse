// Original file: api/superverse/v2/superverse.proto


export interface GetReleaseBuildLogResponse {
  'lines'?: (string)[];
  'nextCursor'?: (string);
  'complete'?: (boolean);
  'retryAfterMs'?: (number);
}

export interface GetReleaseBuildLogResponse__Output {
  'lines': (string)[];
  'nextCursor': (string);
  'complete': (boolean);
  'retryAfterMs': (number);
}
