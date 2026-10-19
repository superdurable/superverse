// Original file: api/superverse/v2/superverse.proto


export interface FlowApiDocument {
  'json'?: (Buffer | Uint8Array | string);
  'httpStatus'?: (number);
  'retryAfterSeconds'?: (number);
}

export interface FlowApiDocument__Output {
  'json': (Buffer);
  'httpStatus': (number);
  'retryAfterSeconds': (number);
}
