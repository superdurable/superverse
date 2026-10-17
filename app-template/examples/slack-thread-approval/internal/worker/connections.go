package worker

import (
	"fmt"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-template-basic-process/internal/connectorconfiguration"
	"github.com/superdurable/dex-template-basic-process/internal/process"
)

// Connections holds one opened connection per dex-app.yaml connection. The
// Worker opens them from the project configuration; API tests pass
// connections without credentials instead.
type Connections struct {
	// SlackApprovals is the slack-approvals connection of SlackThreadApprovalFlow.
	SlackApprovals slack.Connection
}

// OpenConnections opens every declared connection with its connector's
// generated NewProjectConnection. Add one field and one line per connection.
func OpenConnections(configuration connectorconfiguration.Configuration) (Connections, error) {
	slackApprovals, err := slack.NewProjectConnection(configuration.Project(), process.SlackApprovalsConnectionName)
	if err != nil {
		return Connections{}, fmt.Errorf("configure %s connection: %w", process.SlackApprovalsConnectionName, err)
	}
	return Connections{SlackApprovals: slackApprovals}, nil
}
