package worker

import (
	"fmt"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-template-basic-process/internal/connectorconfiguration"
	"github.com/superdurable/dex-template-basic-process/internal/process"
)

// Connections holds one opened connection per dex-app.yaml connection. The
// Worker opens them from the project configuration; API tests pass the
// connector's mocks instead.
type Connections struct {
	// SummaryModel is the summary-model connection of TextSummaryFlow.
	SummaryModel llm.Connection
}

// OpenConnections opens every declared connection with its connector's
// generated NewProjectConnection. Add one field and one line per connection.
func OpenConnections(configuration connectorconfiguration.Configuration) (Connections, error) {
	summaryModel, err := llm.NewProjectConnection(configuration.Project(), process.TextSummaryConnectionName)
	if err != nil {
		return Connections{}, fmt.Errorf("configure %s connection: %w", process.TextSummaryConnectionName, err)
	}
	return Connections{SummaryModel: summaryModel}, nil
}
