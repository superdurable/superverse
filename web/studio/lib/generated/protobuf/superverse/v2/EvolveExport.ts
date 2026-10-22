// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

export interface EvolveExport {
  'parts'?: (number);
  'runs'?: (number);
  'bytes'?: (number | string | Long);
  'truncated'?: (boolean);
  'deleted'?: (boolean);
}

export interface EvolveExport__Output {
  'parts': (number);
  'runs': (number);
  'bytes': (string);
  'truncated': (boolean);
  'deleted': (boolean);
}
