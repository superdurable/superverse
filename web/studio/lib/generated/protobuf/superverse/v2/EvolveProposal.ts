// Original file: api/superverse/v2/superverse.proto

import type { EvolveProposalKind as _superverse_v2_EvolveProposalKind, EvolveProposalKind__Output as _superverse_v2_EvolveProposalKind__Output } from '../../superverse/v2/EvolveProposalKind';
import type { EvolveProposalStatus as _superverse_v2_EvolveProposalStatus, EvolveProposalStatus__Output as _superverse_v2_EvolveProposalStatus__Output } from '../../superverse/v2/EvolveProposalStatus';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { EvolveProposalDecisionRecord as _superverse_v2_EvolveProposalDecisionRecord, EvolveProposalDecisionRecord__Output as _superverse_v2_EvolveProposalDecisionRecord__Output } from '../../superverse/v2/EvolveProposalDecisionRecord';
import type { Long } from '@grpc/proto-loader';

export interface EvolveProposal {
  'reviewId'?: (string);
  'kind'?: (_superverse_v2_EvolveProposalKind);
  'undoes'?: (string);
  'title'?: (string);
  'summary'?: (string);
  'agentId'?: (string);
  'backtestJson'?: (string);
  'status'?: (_superverse_v2_EvolveProposalStatus);
  'statusAt'?: (_google_protobuf_Timestamp | null);
  'revision'?: (number | string | Long);
  'offeredAt'?: (_google_protobuf_Timestamp | null);
  'notifyFailed'?: (boolean);
  'decision'?: (_superverse_v2_EvolveProposalDecisionRecord | null);
  'appliedCommit'?: (string);
  'appliedAt'?: (_google_protobuf_Timestamp | null);
  'liveRevision'?: (number | string | Long);
  'liveAt'?: (_google_protobuf_Timestamp | null);
  'previousLiveReleaseId'?: (string);
}

export interface EvolveProposal__Output {
  'reviewId': (string);
  'kind': (_superverse_v2_EvolveProposalKind__Output);
  'undoes': (string);
  'title': (string);
  'summary': (string);
  'agentId': (string);
  'backtestJson': (string);
  'status': (_superverse_v2_EvolveProposalStatus__Output);
  'statusAt': (_google_protobuf_Timestamp__Output | null);
  'revision': (string);
  'offeredAt': (_google_protobuf_Timestamp__Output | null);
  'notifyFailed': (boolean);
  'decision': (_superverse_v2_EvolveProposalDecisionRecord__Output | null);
  'appliedCommit': (string);
  'appliedAt': (_google_protobuf_Timestamp__Output | null);
  'liveRevision': (string);
  'liveAt': (_google_protobuf_Timestamp__Output | null);
  'previousLiveReleaseId': (string);
}
