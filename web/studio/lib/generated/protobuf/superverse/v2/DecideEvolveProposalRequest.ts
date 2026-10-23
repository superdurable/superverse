// Original file: api/superverse/v2/superverse.proto

import type { EvolveProposalDecision as _superverse_v2_EvolveProposalDecision, EvolveProposalDecision__Output as _superverse_v2_EvolveProposalDecision__Output } from '../../superverse/v2/EvolveProposalDecision';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface DecideEvolveProposalRequest {
  'projectId'?: (string);
  'reviewId'?: (string);
  'revision'?: (number | string | Long);
  'decision'?: (_superverse_v2_EvolveProposalDecision);
  'reason'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface DecideEvolveProposalRequest__Output {
  'projectId': (string);
  'reviewId': (string);
  'revision': (string);
  'decision': (_superverse_v2_EvolveProposalDecision__Output);
  'reason': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
