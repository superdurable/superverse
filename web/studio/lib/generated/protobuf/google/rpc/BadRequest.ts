// Original file: web/studio/proto/google/rpc/error_details.proto


export interface _google_rpc_BadRequest_FieldViolation {
  'field'?: (string);
  'description'?: (string);
}

export interface _google_rpc_BadRequest_FieldViolation__Output {
  'field': (string);
  'description': (string);
}

export interface BadRequest {
  'fieldViolations'?: (_google_rpc_BadRequest_FieldViolation)[];
}

export interface BadRequest__Output {
  'fieldViolations': (_google_rpc_BadRequest_FieldViolation__Output)[];
}
