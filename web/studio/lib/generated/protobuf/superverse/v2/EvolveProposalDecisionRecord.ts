// Original file: api/superverse/v2/superverse.proto

import type { EvolveProposalDecision as _superverse_v2_EvolveProposalDecision, EvolveProposalDecision__Output as _superverse_v2_EvolveProposalDecision__Output } from '../../superverse/v2/EvolveProposalDecision';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface EvolveProposalDecisionRecord {
  'decision'?: (_superverse_v2_EvolveProposalDecision);
  'actorAccountId'?: (string);
  'decidedAt'?: (_google_protobuf_Timestamp | null);
  'reason'?: (string);
}

export interface EvolveProposalDecisionRecord__Output {
  'decision': (_superverse_v2_EvolveProposalDecision__Output);
  'actorAccountId': (string);
  'decidedAt': (_google_protobuf_Timestamp__Output | null);
  'reason': (string);
}
