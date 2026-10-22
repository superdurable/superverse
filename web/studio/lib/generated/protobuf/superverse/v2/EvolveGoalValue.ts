// Original file: api/superverse/v2/superverse.proto


export interface EvolveGoalValue {
  'goalId'?: (string);
  'value'?: (number | string);
  'runs'?: (number);
  'fewRuns'?: (boolean);
  'holds'?: (boolean);
  '_value'?: "value";
  '_holds'?: "holds";
}

export interface EvolveGoalValue__Output {
  'goalId': (string);
  'value'?: (number);
  'runs': (number);
  'fewRuns': (boolean);
  'holds'?: (boolean);
  '_value'?: "value";
  '_holds'?: "holds";
}
