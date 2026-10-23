package connectorconfiguration

import "fmt"

// ConnectionNotConfiguredCode is the stable unavailable-feature result code.
const ConnectionNotConfiguredCode = "connection_not_configured"

// ConnectionNotConfiguredError is a user action, not a failed provider call.
// Code is stable; Error is safe to show and contains no stored settings.
type ConnectionNotConfiguredError struct {
	ConnectionName string
}

func (*ConnectionNotConfiguredError) Code() string { return ConnectionNotConfiguredCode }
func (e *ConnectionNotConfiguredError) Error() string {
	return fmt.Sprintf("Connect %s to use this feature.", e.ConnectionName)
}

// RequireConnection inspects the already verified immutable snapshot. A
// declaration with no selected entry is absent; malformed selected data is
// left to the connector's ordinary opener and must not become optional.
func (configuration Configuration) RequireConnection(connectorID, name string) error {
	if configuration.project == nil {
		return fmt.Errorf("loaded project configuration is required")
	}
	for _, entry := range configuration.project.Configuration.Connections {
		if entry.ConnectorID == connectorID && entry.ConnectionName == name && entry.SharedConnectionID != "" {
			return nil
		}
	}
	return &ConnectionNotConfiguredError{ConnectionName: name}
}
