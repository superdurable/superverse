// Original file: api/superverse/v2/superverse.proto

import type { EvolveProposalRefusal as _superverse_v2_EvolveProposalRefusal, EvolveProposalRefusal__Output as _superverse_v2_EvolveProposalRefusal__Output } from '../../superverse/v2/EvolveProposalRefusal';
import type { EvolveProposal as _superverse_v2_EvolveProposal, EvolveProposal__Output as _superverse_v2_EvolveProposal__Output } from '../../superverse/v2/EvolveProposal';

export interface DecideEvolveProposalResponse {
  'refusal'?: (_superverse_v2_EvolveProposalRefusal);
  'proposal'?: (_superverse_v2_EvolveProposal | null);
}

export interface DecideEvolveProposalResponse__Output {
  'refusal': (_superverse_v2_EvolveProposalRefusal__Output);
  'proposal': (_superverse_v2_EvolveProposal__Output | null);
}
