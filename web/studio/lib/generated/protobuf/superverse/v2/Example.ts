// Original file: api/superverse/v2/superverse.proto

import type { ExampleCategory as _superverse_v2_ExampleCategory, ExampleCategory__Output as _superverse_v2_ExampleCategory__Output } from '../../superverse/v2/ExampleCategory';

export interface Example {
  'exampleId'?: (string);
  'title'?: (string);
  'category'?: (_superverse_v2_ExampleCategory);
  'apps'?: (string)[];
  'decider'?: (string);
  'description'?: (string);
  'connectorIds'?: (string)[];
}

export interface Example__Output {
  'exampleId': (string);
  'title': (string);
  'category': (_superverse_v2_ExampleCategory__Output);
  'apps': (string)[];
  'decider': (string);
  'description': (string);
  'connectorIds': (string)[];
}
