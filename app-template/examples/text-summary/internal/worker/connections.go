package worker

import (
	"errors"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-template-basic-process/internal/connectorconfiguration"
	"github.com/superdurable/dex-template-basic-process/internal/process"
)

// Connections holds one opened connection per dex-app.yaml connection. The
// Worker opens them from the project configuration; API tests pass the
// connector's mocks instead.
type Connections struct {
	// SummaryModel is the summary-model connection of TextSummaryFlow.
	SummaryModel        llm.Connection
	SummaryModelMissing *connectorconfiguration.ConnectionNotConfiguredError
}

// OpenConnections opens every declared connection with its connector's
// generated NewProjectConnection. Add one field and one line per connection.
func OpenConnections(configuration connectorconfiguration.Configuration) (Connections, error) {
	if err := configuration.RequireConnection(llm.ConnectorID, process.TextSummaryConnectionName); err != nil {
		var missing *connectorconfiguration.ConnectionNotConfiguredError
		if !errors.As(err, &missing) {
			return Connections{}, err
		}
		// This binding preserves the generated Step's immutable metadata.
		// PrepareSummary routes typed absence before invoking any operation.
		connection, err := llm.NewConnectionWithOperations(unavailableSummary{}, sdkgo.ConnectionRef{Provider: llm.ConnectorID, Name: process.TextSummaryConnectionName})
		return Connections{SummaryModel: connection, SummaryModelMissing: missing}, err
	}
	httpClient, err := configuration.LocalModelHTTPClient()
	if err != nil {
		return Connections{}, err
	}
	var options []llm.Option
	if httpClient != nil {
		options = append(options, llm.WithHTTPClient(httpClient))
	}
	summaryModel, err := llm.NewProjectConnection(configuration.Project(), process.TextSummaryConnectionName, options...)
	if err != nil {
		return Connections{}, fmt.Errorf("configure %s connection: %w", process.TextSummaryConnectionName, err)
	}
	return Connections{SummaryModel: summaryModel}, nil
}

// unavailableSummary exposes the released operation definition for registration.
// It never contacts a provider, even if a future caller bypasses the Flow guard.
type unavailableSummary struct{}

func (unavailableSummary) GenerateText() sdkgo.Query[llm.GenerateTextRequest, llm.GenerateTextResponse] {
	return unavailableSummaryQuery{}
}

type unavailableSummaryQuery struct{}

func (unavailableSummaryQuery) Definition() sdkgo.QueryDefinition { return llm.GenerateTextDefinition }
func (unavailableSummaryQuery) Invoke(sdkgo.Call, llm.GenerateTextRequest) sdkgo.QueryAttempt[llm.GenerateTextResponse] {
	failure := sdkgo.Failure{Kind: sdkgo.FailureLocalDefect, Provider: llm.ConnectorID, Operation: "generateText", Message: "Connect summary-model to use this feature."}
	return sdkgo.NewQueryBranch(sdkgo.DefectBranchID, llm.GenerateTextResponse{}, &failure, sdkgo.Receipt{})
}
