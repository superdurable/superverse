// Original file: api/superverse/v2/superverse.proto

import type { ConnectorRequirement as _superverse_v2_ConnectorRequirement, ConnectorRequirement__Output as _superverse_v2_ConnectorRequirement__Output } from '../../superverse/v2/ConnectorRequirement';
import type { Struct as _google_protobuf_Struct, Struct__Output as _google_protobuf_Struct__Output } from '../../google/protobuf/Struct';
import type { ObjectReference as _superverse_v2_ObjectReference, ObjectReference__Output as _superverse_v2_ObjectReference__Output } from '../../superverse/v2/ObjectReference';
import type { Long } from '@grpc/proto-loader';

export interface AppManifestSnapshot {
  'sourceCommit'?: (string);
  'manifestDigest'?: (string);
  'connectorContractDigest'?: (string);
  'environmentContractDigest'?: (string);
  'revision'?: (number | string | Long);
  'connectors'?: (_superverse_v2_ConnectorRequirement)[];
  'environment'?: (_google_protobuf_Struct | null);
  'manifestObject'?: (_superverse_v2_ObjectReference | null);
  'connectorContractObject'?: (_superverse_v2_ObjectReference | null);
  'environmentContractObject'?: (_superverse_v2_ObjectReference | null);
}

export interface AppManifestSnapshot__Output {
  'sourceCommit': (string);
  'manifestDigest': (string);
  'connectorContractDigest': (string);
  'environmentContractDigest': (string);
  'revision': (string);
  'connectors': (_superverse_v2_ConnectorRequirement__Output)[];
  'environment': (_google_protobuf_Struct__Output | null);
  'manifestObject': (_superverse_v2_ObjectReference__Output | null);
  'connectorContractObject': (_superverse_v2_ObjectReference__Output | null);
  'environmentContractObject': (_superverse_v2_ObjectReference__Output | null);
}
