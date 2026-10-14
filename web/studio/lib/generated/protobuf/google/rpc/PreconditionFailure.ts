// Original file: web/studio/proto/google/rpc/error_details.proto


export interface _google_rpc_PreconditionFailure_Violation {
  'type'?: (string);
  'subject'?: (string);
  'description'?: (string);
}

export interface _google_rpc_PreconditionFailure_Violation__Output {
  'type': (string);
  'subject': (string);
  'description': (string);
}

export interface PreconditionFailure {
  'violations'?: (_google_rpc_PreconditionFailure_Violation)[];
}

export interface PreconditionFailure__Output {
  'violations': (_google_rpc_PreconditionFailure_Violation__Output)[];
}
