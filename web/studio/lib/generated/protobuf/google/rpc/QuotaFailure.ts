// Original file: web/studio/proto/google/rpc/error_details.proto


export interface _google_rpc_QuotaFailure_Violation {
  'subject'?: (string);
  'description'?: (string);
}

export interface _google_rpc_QuotaFailure_Violation__Output {
  'subject': (string);
  'description': (string);
}

export interface QuotaFailure {
  'violations'?: (_google_rpc_QuotaFailure_Violation)[];
}

export interface QuotaFailure__Output {
  'violations': (_google_rpc_QuotaFailure_Violation__Output)[];
}
