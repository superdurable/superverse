// Original file: api/superverse/v2/superverse.proto

import type { Struct as _google_protobuf_Struct, Struct__Output as _google_protobuf_Struct__Output } from '../../google/protobuf/Struct';

export interface FlowDefinition {
  'flowType'?: (string);
  'definition'?: (_google_protobuf_Struct | null);
  'sourcePath'?: (string);
  'schemaVersion'?: (string);
  'digest'?: (string);
  'valid'?: (boolean);
}

export interface FlowDefinition__Output {
  'flowType': (string);
  'definition': (_google_protobuf_Struct__Output | null);
  'sourcePath': (string);
  'schemaVersion': (string);
  'digest': (string);
  'valid': (boolean);
}
