// Original file: api/superverse/v2/superverse.proto


export interface ConnectorCatalogEntry {
  'connectorId'?: (string);
  'displayName'?: (string);
  'description'?: (string);
  'modulePath'?: (string);
  'version'?: (string);
  'versionLine'?: (string);
  'logoSvg'?: (string);
  'endpointBound'?: (boolean);
  'endpointBoundReason'?: (string);
}

export interface ConnectorCatalogEntry__Output {
  'connectorId': (string);
  'displayName': (string);
  'description': (string);
  'modulePath': (string);
  'version': (string);
  'versionLine': (string);
  'logoSvg': (string);
  'endpointBound': (boolean);
  'endpointBoundReason': (string);
}
