package worker

import (
	"github.com/superdurable/dex-template-basic-process/internal/process"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Application is the single list of the application's Flows with their
// connections: the Worker and the API tests register exactly this list. List
// every Flow here and its source file in dex-app.yaml.
func Application(connections Connections) []dex.Flow {
	return []dex.Flow{
		process.TextSummaryFlow{Connection: connections.SummaryModel},
	}
}
