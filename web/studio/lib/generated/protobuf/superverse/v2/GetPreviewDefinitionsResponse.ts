// Original file: api/superverse/v2/superverse.proto

import type { PreviewSnapshot as _superverse_v2_PreviewSnapshot, PreviewSnapshot__Output as _superverse_v2_PreviewSnapshot__Output } from '../../superverse/v2/PreviewSnapshot';
import type { FlowDefinition as _superverse_v2_FlowDefinition, FlowDefinition__Output as _superverse_v2_FlowDefinition__Output } from '../../superverse/v2/FlowDefinition';

export interface GetPreviewDefinitionsResponse {
  'preview'?: (_superverse_v2_PreviewSnapshot | null);
  'flowDefinitions'?: (_superverse_v2_FlowDefinition)[];
}

export interface GetPreviewDefinitionsResponse__Output {
  'preview': (_superverse_v2_PreviewSnapshot__Output | null);
  'flowDefinitions': (_superverse_v2_FlowDefinition__Output)[];
}
