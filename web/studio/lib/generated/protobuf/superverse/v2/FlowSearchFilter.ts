// Original file: api/superverse/v2/superverse.proto


export interface FlowSearchFilter {
  'field'?: (string);
  'operator'?: (string);
  'valuesJson'?: (Buffer | Uint8Array | string);
}

export interface FlowSearchFilter__Output {
  'field': (string);
  'operator': (string);
  'valuesJson': (Buffer);
}
